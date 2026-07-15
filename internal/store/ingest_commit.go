package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"gorm.io/gorm"
)

func (repository *Repository) CommitIngestBatch(
	ctx context.Context,
	batch IngestBatch,
) (GenerationCursor, error) {
	if repository == nil || repository.database == nil {
		return GenerationCursor{}, ErrInvalidRepository
	}
	facts, err := repository.marshalIngestFacts(batch)
	if err != nil {
		return GenerationCursor{}, err
	}
	seed, projector, err := marshalParserCheckpoint(batch.Checkpoint)
	if err != nil {
		return GenerationCursor{}, err
	}
	var cursor GenerationCursor
	err = repository.WithinWriteUnit(ctx, func(unit *WriteUnit) error {
		var err error
		cursor, err = unit.commitIngestBatch(batch, facts, seed, projector)
		return err
	})
	return cursor, err
}

func (repository *Repository) marshalIngestFacts(batch IngestBatch) ([]byte, error) {
	if err := repository.validateIngestBatch(batch); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(batch.Facts)
	if err != nil {
		return nil, invalidRecord("marshal ingest facts")
	}
	if len(encoded) > maxParserCheckpointBytes {
		return nil, invalidRecord("ingest facts exceed batch size limit")
	}
	return encoded, nil
}

func (repository *Repository) validateIngestBatch(batch IngestBatch) error {
	if batch.SourceFileID == "" || batch.Generation < 0 || batch.PreviousCommittedOffset < 0 ||
		batch.AtMS < 0 || batch.Fingerprint.SourceFileID != batch.SourceFileID ||
		!validSourceFingerprint(batch.Fingerprint) || batch.Checkpoint.Version != ParserCheckpointVersion ||
		batch.Checkpoint.ParserVersion == "" || batch.Checkpoint.CommittedOffset < batch.PreviousCommittedOffset ||
		batch.Checkpoint.CommittedOffset > batch.Fingerprint.SizeBytes ||
		(batch.Checkpoint.CommittedOffset == batch.PreviousCommittedOffset && !batch.EOF) {
		return invalidRecord("ingest batch header is invalid")
	}
	if batch.PreviousFingerprint != nil &&
		(!validSourceFingerprint(*batch.PreviousFingerprint) ||
			batch.PreviousFingerprint.SourceFileID != batch.SourceFileID) {
		return invalidRecord("ingest previous fingerprint is invalid")
	}
	if err := validateParserCheckpoint(batch.Checkpoint); err != nil {
		return err
	}
	var sessionID string
	if batch.Checkpoint.Seed != nil && batch.Checkpoint.Seed.Session != nil {
		sessionID = batch.Checkpoint.Seed.Session.SessionID
	}
	canonicalSessionSourceKind := batch.Checkpoint.Projector.SessionSourceKind
	for _, facts := range batch.Facts {
		if err := repository.validateBatch(facts); err != nil {
			return err
		}
		factSessionID, err := batchSessionID(facts)
		if err != nil {
			return err
		}
		if facts.Usage != nil && factSessionID == "" {
			return invalidRecord("ingest turn usage is missing session identity")
		}
		if factSessionID != "" && (sessionID == "" || factSessionID != sessionID) {
			return invalidRecord("ingest facts do not match checkpoint session")
		}
		if facts.Session != nil && canonicalSessionSourceKind != "" &&
			facts.Session.SourceKind != canonicalSessionSourceKind {
			return invalidRecord("ingest session fact conflicts with canonical source kind")
		}
		if facts.QuotaObservation != nil &&
			(facts.QuotaObservation.Source != QuotaSourceLocalJSONL ||
				facts.QuotaObservation.SourceFileID == nil ||
				*facts.QuotaObservation.SourceFileID != batch.SourceFileID) {
			return invalidRecord("ingest quota observation does not match source file")
		}
		if facts.Turn != nil && facts.Turn.SourceGeneration != batch.Generation ||
			facts.Usage != nil && facts.Usage.SourceGeneration != batch.Generation ||
			facts.SessionUsageCurrent != nil && facts.SessionUsageCurrent.SourceGeneration != batch.Generation ||
			facts.QuotaObservation != nil && facts.QuotaObservation.SourceGeneration != batch.Generation {
			return invalidRecord("ingest fact generation does not match batch")
		}
		if facts.Turn != nil && (facts.Turn.StartOffset > batch.Checkpoint.CommittedOffset ||
			(facts.Turn.CompleteOffset != nil && *facts.Turn.CompleteOffset > batch.Checkpoint.CommittedOffset)) ||
			facts.Usage != nil && facts.Usage.SourceOffset > batch.Checkpoint.CommittedOffset ||
			facts.SessionUsageCurrent != nil &&
				facts.SessionUsageCurrent.SourceOffset > batch.Checkpoint.CommittedOffset ||
			facts.QuotaObservation != nil &&
				facts.QuotaObservation.SourceOffset > batch.Checkpoint.CommittedOffset {
			return invalidRecord("ingest fact source position exceeds checkpoint")
		}
	}
	for _, diagnostic := range batch.Diagnostics {
		if !validIngestDiagnostic(diagnostic, batch.Checkpoint.CommittedOffset) {
			return invalidRecord("ingest diagnostic is invalid")
		}
	}
	if batch.Checkpoint.Projector.SessionUsage != nil &&
		batch.Checkpoint.Projector.SessionUsage.SourceGeneration != batch.Generation {
		return invalidRecord("projector usage generation does not match batch")
	}
	for _, turn := range batch.Checkpoint.Projector.OpenTurns {
		if turn.SourceGeneration != batch.Generation {
			return invalidRecord("projector open turn generation does not match batch")
		}
	}
	if batch.JobTransition != nil && batch.JobTransition.AtMS != batch.AtMS {
		return invalidRecord("job transition time does not match ingest batch")
	}
	return nil
}

func (unit *WriteUnit) commitIngestBatch(
	batch IngestBatch,
	facts []byte,
	seed []byte,
	projector []byte,
) (GenerationCursor, error) {
	if err := unit.requireActive(); err != nil {
		return GenerationCursor{}, err
	}
	database := unit.transaction.WithContext(unit.ctx)
	current, found, err := generationCursorByID(
		unit.ctx, database, batch.SourceFileID, batch.Generation,
	)
	if err != nil {
		return GenerationCursor{}, err
	}
	if !found {
		return GenerationCursor{}, invalidRecord("ingest generation does not exist")
	}
	storedGeneration, found, err := generationByID(
		unit.ctx, database, batch.SourceFileID, batch.Generation,
	)
	if err != nil {
		return GenerationCursor{}, err
	}
	if !found {
		return GenerationCursor{}, invalidRecord("ingest generation does not exist")
	}
	if current.ParserVersion != batch.Checkpoint.ParserVersion {
		return GenerationCursor{}, invalidRecord("ingest generation fingerprint or parser version changed")
	}
	if current.Checkpoint.Projector.SessionSourceKind != "" &&
		current.Checkpoint.Projector.SessionSourceKind != batch.Checkpoint.Projector.SessionSourceKind {
		return GenerationCursor{}, invalidRecord("ingest canonical session source kind changed")
	}
	currentSessionID := checkpointSessionID(current.Checkpoint)
	incomingSessionID := checkpointSessionID(batch.Checkpoint)
	if currentSessionID != "" && currentSessionID != incomingSessionID ||
		storedGeneration.SessionID != nil && *storedGeneration.SessionID != incomingSessionID {
		return GenerationCursor{}, invalidRecord("ingest generation session identity changed")
	}
	fingerprintChanged := !sourceFingerprintEqual(current.Fingerprint, batch.Fingerprint)
	if fingerprintChanged &&
		(current.State != GenerationActive || batch.PreviousFingerprint == nil ||
			!sourceFingerprintEqual(current.Fingerprint, *batch.PreviousFingerprint) ||
			!validAppendFingerprintProgression(current.Fingerprint, batch.Fingerprint)) {
		return GenerationCursor{}, invalidRecord("ingest generation fingerprint or parser version changed")
	}
	if !fingerprintChanged && current.Checkpoint.CommittedOffset == batch.Checkpoint.CommittedOffset {
		receiptExists, err := committedBatchReceiptExists(database, batch)
		if err != nil {
			return GenerationCursor{}, err
		}
		if receiptExists {
			if err := unit.validateCommittedBatchReplay(batch, facts); err != nil {
				return GenerationCursor{}, err
			}
			if batch.JobTransition != nil {
				if err := unit.TransitionJobRun(*batch.JobTransition); err != nil {
					return GenerationCursor{}, err
				}
			}
			return current, nil
		}
		if current.State != GenerationBuilding ||
			batch.PreviousCommittedOffset != current.Checkpoint.CommittedOffset || !batch.EOF {
			return GenerationCursor{}, invalidRecord("ingest replay receipt is unavailable")
		}
	}
	if batch.AtMS <= storedGeneration.UpdatedAtMS {
		return GenerationCursor{}, invalidRecord("ingest commit time does not advance")
	}
	if current.Checkpoint.CommittedOffset != batch.PreviousCommittedOffset {
		return GenerationCursor{}, invalidRecord("ingest committed offset compare-and-swap failed")
	}
	receipt := sourceGenerationBatchModel{
		SourceFileID: batch.SourceFileID, Generation: batch.Generation,
		FromOffset: batch.PreviousCommittedOffset, ToOffset: batch.Checkpoint.CommittedOffset,
		BatchIdentitySHA256: ingestBatchIdentityDigest(batch),
		Facts:               facts, EOF: batch.EOF, CreatedAtMS: batch.AtMS,
	}
	if err := database.Create(&receipt).Error; err != nil {
		return GenerationCursor{}, err
	}
	if err := createIngestDiagnostics(database, batch); err != nil {
		return GenerationCursor{}, err
	}
	if err := database.Model(&parserCheckpointModel{}).
		Where("source_file_id = ? AND generation = ?", batch.SourceFileID, batch.Generation).
		Updates(map[string]any{
			"checkpoint_version": batch.Checkpoint.Version,
			"parser_seed":        seed, "projector_state": projector, "updated_at_ms": batch.AtMS,
		}).Error; err != nil {
		return GenerationCursor{}, err
	}
	if err := database.Model(&sourceGenerationModel{}).
		Where("source_file_id = ? AND generation = ?", batch.SourceFileID, batch.Generation).
		Updates(sourceGenerationProgressUpdates(batch)).Error; err != nil {
		return GenerationCursor{}, err
	}

	state := current.State
	switch state {
	case GenerationActive:
		if fingerprintChanged {
			// 依赖查询必须使用事务开始时观察到的旧 snapshot；当前 generation
			// 的进度行此时已写入新 fingerprint。
			active := sourceGenerationModel{
				SourceFileID: current.SourceFileID, Generation: current.Generation,
				FingerprintSHA256: current.Fingerprint.FingerprintSHA256,
			}
			if err := unit.supersedeDependentBuildingGenerations(
				database, active, nil, batch.AtMS,
			); err != nil {
				return GenerationCursor{}, err
			}
		}
		for _, fact := range batch.Facts {
			if err := unit.UpsertFacts(fact); err != nil {
				return GenerationCursor{}, err
			}
		}
		if err := unit.advanceActiveSourceFile(batch); err != nil {
			return GenerationCursor{}, err
		}
	case GenerationBuilding:
		if batch.EOF {
			if err := unit.activateBuildingGeneration(batch); err != nil {
				return GenerationCursor{}, err
			}
			state = GenerationActive
		}
	default:
		return GenerationCursor{}, invalidRecord("superseded generation cannot accept ingest")
	}
	if batch.JobTransition != nil {
		if err := unit.TransitionJobRun(*batch.JobTransition); err != nil {
			return GenerationCursor{}, err
		}
	}
	if state == GenerationActive {
		if err := database.Where(
			"source_file_id = ? AND generation = ? AND NOT (from_offset = ? AND to_offset = ? AND batch_identity_sha256 = ?)",
			batch.SourceFileID, batch.Generation, batch.PreviousCommittedOffset,
			batch.Checkpoint.CommittedOffset, ingestBatchIdentityDigest(batch),
		).Delete(&sourceGenerationBatchModel{}).Error; err != nil {
			return GenerationCursor{}, err
		}
	}
	updated, _, err := generationCursorByID(unit.ctx, database, batch.SourceFileID, batch.Generation)
	return updated, err
}

func committedBatchReceiptExists(database *gorm.DB, batch IngestBatch) (bool, error) {
	var count int64
	err := database.Model(&sourceGenerationBatchModel{}).Where(
		"source_file_id = ? AND generation = ? AND from_offset = ? AND to_offset = ? AND batch_identity_sha256 = ?",
		batch.SourceFileID, batch.Generation, batch.PreviousCommittedOffset,
		batch.Checkpoint.CommittedOffset, ingestBatchIdentityDigest(batch),
	).Count(&count).Error
	return count > 0, err
}

func validAppendFingerprintProgression(previous, current SourceFingerprint) bool {
	return previous.SourceFileID == current.SourceFileID && previous.Provider == current.Provider &&
		previous.DeviceID == current.DeviceID && previous.Inode == current.Inode &&
		current.SizeBytes >= previous.SizeBytes &&
		current.MTimeNS >= previous.MTimeNS && current.PrefixBytes >= previous.PrefixBytes &&
		(current.PrefixBytes != previous.PrefixBytes || current.PrefixSHA256 == previous.PrefixSHA256)
}

func (unit *WriteUnit) validateCommittedBatchReplay(batch IngestBatch, facts []byte) error {
	database := unit.transaction.WithContext(unit.ctx)
	var receipt sourceGenerationBatchModel
	if err := database.Where(
		"source_file_id = ? AND generation = ? AND from_offset = ? AND to_offset = ? AND batch_identity_sha256 = ?",
		batch.SourceFileID, batch.Generation, batch.PreviousCommittedOffset,
		batch.Checkpoint.CommittedOffset, ingestBatchIdentityDigest(batch),
	).Take(&receipt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invalidRecord("ingest replay receipt is unavailable")
		}
		return err
	}
	if !bytes.Equal(receipt.Facts, facts) || receipt.EOF != batch.EOF {
		return invalidRecord("ingest replay conflicts with committed facts")
	}
	cursor, found, err := generationCursorByID(unit.ctx, database, batch.SourceFileID, batch.Generation)
	if err != nil {
		return err
	}
	if !found || !reflect.DeepEqual(cursor.Checkpoint, batch.Checkpoint) ||
		!sourceFingerprintEqual(cursor.Fingerprint, batch.Fingerprint) {
		return invalidRecord("ingest replay conflicts with committed checkpoint")
	}
	storedDiagnostics, err := ingestDiagnosticsForBatch(
		unit.ctx, database, batch.SourceFileID, batch.Generation, batch.Checkpoint.CommittedOffset,
		ingestBatchIdentityDigest(batch),
	)
	if err != nil {
		return err
	}
	if !slices.Equal(storedDiagnostics, batch.Diagnostics) {
		return invalidRecord("ingest replay conflicts with committed diagnostics")
	}
	return nil
}

func (unit *WriteUnit) activateBuildingGeneration(batch IngestBatch) error {
	database := unit.transaction.WithContext(unit.ctx)
	sessionID := checkpointSessionID(batch.Checkpoint)
	building, found, err := generationByID(unit.ctx, database, batch.SourceFileID, batch.Generation)
	if err != nil {
		return err
	}
	if !found || GenerationState(building.State) != GenerationBuilding {
		return invalidRecord("building generation is unavailable for activation")
	}
	base, hasBase, err := unit.baseGenerationForActivation(database, building)
	if err != nil {
		return err
	}
	baseSessionID, err := unit.baseGenerationSessionID(database, base, hasBase)
	if err != nil {
		return err
	}
	if baseSessionID != nil {
		if err := ensureExclusiveActiveSessionOwner(database, base, *baseSessionID); err != nil {
			return err
		}
		// Session 是一个 generation 的 authoritative aggregate；删除后由 staged facts 完整重建。
		if err := database.Delete(&sessionModel{}, "session_id = ?", *baseSessionID).Error; err != nil {
			return err
		}
	}
	var staged []sourceGenerationBatchModel
	if err := database.Where("source_file_id = ? AND generation = ?", batch.SourceFileID, batch.Generation).
		Order("from_offset").Order("to_offset").Find(&staged).Error; err != nil {
		return err
	}
	for _, stagedBatch := range staged {
		facts, err := unit.repository.unmarshalIngestFacts(stagedBatch.Facts)
		if err != nil {
			return err
		}
		for _, fact := range facts {
			if err := unit.UpsertFacts(fact); err != nil {
				return err
			}
		}
	}
	if hasBase {
		if err := casSupersedeBaseGeneration(database, base, batch.AtMS); err != nil {
			return err
		}
		if err := unit.supersedeDependentBuildingGenerations(
			database, base, &building, batch.AtMS,
		); err != nil {
			return err
		}
	} else {
		active, found, err := generationByState(unit.ctx, database, batch.SourceFileID, GenerationActive)
		if err != nil {
			return err
		}
		if found && (active.SourceFileID != building.SourceFileID || active.Generation != building.Generation) {
			return invalidRecord("initial generation activation found an unexpected active generation")
		}
		if err := unit.supersedeInitialSiblingGenerations(database, building, batch.AtMS); err != nil {
			return err
		}
	}
	result := database.Model(&sourceGenerationModel{}).
		Where("source_file_id = ? AND generation = ?", batch.SourceFileID, batch.Generation).
		Where("state = ?", string(GenerationBuilding)).
		Updates(map[string]any{
			"state": string(GenerationActive), "session_id": nullableNonEmptyString(sessionID),
			"updated_at_ms": batch.AtMS,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return invalidRecord("building generation activation compare-and-swap failed")
	}
	if err := unit.advanceActiveSourceFile(batch); err != nil {
		return err
	}
	if batch.Checkpoint.Seed != nil && batch.Checkpoint.Seed.Session != nil && batch.Checkpoint.Seed.Session.SessionID != sessionID {
		return invalidRecord("activated checkpoint session changed")
	}
	if err := unit.supersedeReplacedSource(batch); err != nil {
		return err
	}
	return nil
}

func (unit *WriteUnit) baseGenerationForActivation(
	database *gorm.DB,
	building sourceGenerationModel,
) (sourceGenerationModel, bool, error) {
	previousBuilding, err := validatedSupersededBuilding(unit.ctx, database, building)
	if err != nil {
		return sourceGenerationModel{}, false, err
	}
	hasPreviousBuilding := hasSupersededBuildingLineage(building)
	hasSource := building.BaseSourceFileID != nil
	hasGeneration := building.BaseGeneration != nil
	hasFingerprint := building.BaseFingerprintSHA256 != nil
	if !hasSource && !hasGeneration && !hasFingerprint {
		if building.ReplacesSourceFileID != nil {
			contains, err := supersededBuildingChainContains(
				unit.ctx, database, building, *building.ReplacesSourceFileID,
			)
			if err != nil {
				return sourceGenerationModel{}, false, err
			}
			if !contains {
				return sourceGenerationModel{}, false, invalidRecord("replacement generation has no persisted lineage")
			}
		}
		return sourceGenerationModel{}, false, nil
	}
	if !hasSource || !hasGeneration || !hasFingerprint {
		return sourceGenerationModel{}, false, invalidRecord("building generation base lineage is incomplete")
	}
	if !validSHA256String(*building.BaseFingerprintSHA256) {
		return sourceGenerationModel{}, false, invalidRecord("building generation base lineage is invalid")
	}
	if hasPreviousBuilding {
		previousBase := generationBaseFromModel(previousBuilding)
		if previousBase == nil || previousBase.SourceFileID != *building.BaseSourceFileID ||
			previousBase.Generation != *building.BaseGeneration ||
			previousBase.FingerprintSHA256 != *building.BaseFingerprintSHA256 {
			return sourceGenerationModel{}, false, invalidRecord("building generation base changed across supersession")
		}
		if previousBuilding.SourceFileID != building.SourceFileID &&
			(building.ReplacesSourceFileID == nil || *building.ReplacesSourceFileID != previousBuilding.SourceFileID) {
			return sourceGenerationModel{}, false, invalidRecord("physical building replacement lineage is invalid")
		}
	} else {
		expectedSourceFileID := building.SourceFileID
		if building.ReplacesSourceFileID != nil {
			expectedSourceFileID = *building.ReplacesSourceFileID
		}
		if *building.BaseSourceFileID != expectedSourceFileID {
			return sourceGenerationModel{}, false, invalidRecord("building generation base lineage is invalid")
		}
	}
	base, found, err := generationByID(
		unit.ctx, database, *building.BaseSourceFileID, *building.BaseGeneration,
	)
	if err != nil {
		return sourceGenerationModel{}, false, err
	}
	if !found || GenerationState(base.State) != GenerationActive ||
		base.FingerprintSHA256 != *building.BaseFingerprintSHA256 {
		return sourceGenerationModel{}, false, invalidRecord("replacement base generation is stale")
	}
	return base, true, nil
}

func hasSupersededBuildingLineage(model sourceGenerationModel) bool {
	return model.SupersededBuildingSourceFileID != nil || model.SupersededBuildingGeneration != nil ||
		model.SupersededBuildingFingerprintSHA256 != nil || model.SupersededBuildingParserVersion != nil
}

func validatedSupersededBuilding(
	ctx context.Context,
	database *gorm.DB,
	model sourceGenerationModel,
) (sourceGenerationModel, error) {
	hasSource := model.SupersededBuildingSourceFileID != nil
	hasGeneration := model.SupersededBuildingGeneration != nil
	hasFingerprint := model.SupersededBuildingFingerprintSHA256 != nil
	hasParser := model.SupersededBuildingParserVersion != nil
	if !hasSource && !hasGeneration && !hasFingerprint && !hasParser {
		return sourceGenerationModel{}, nil
	}
	if !hasSource || !hasGeneration || !hasFingerprint || !hasParser {
		return sourceGenerationModel{}, invalidRecord("superseded building lineage is incomplete")
	}
	previous, found, err := generationByID(
		ctx, database,
		*model.SupersededBuildingSourceFileID, *model.SupersededBuildingGeneration,
	)
	if err != nil {
		return sourceGenerationModel{}, err
	}
	if !found || GenerationState(previous.State) != GenerationSuperseded ||
		previous.FingerprintSHA256 != *model.SupersededBuildingFingerprintSHA256 ||
		previous.ParserVersion != *model.SupersededBuildingParserVersion {
		return sourceGenerationModel{}, invalidRecord("superseded building lineage is stale")
	}
	return previous, nil
}

func supersededBuildingChainContains(
	ctx context.Context,
	database *gorm.DB,
	model sourceGenerationModel,
	targetSourceFileID string,
) (bool, error) {
	seen := make(map[string]struct{})
	current := model
	for current.SupersededBuildingSourceFileID != nil || current.SupersededBuildingGeneration != nil ||
		current.SupersededBuildingFingerprintSHA256 != nil || current.SupersededBuildingParserVersion != nil {
		previous, err := validatedSupersededBuilding(ctx, database, current)
		if err != nil {
			return false, err
		}
		key := previous.SourceFileID + ":" + fmt.Sprintf("%d", previous.Generation)
		if _, exists := seen[key]; exists {
			return false, invalidRecord("superseded building lineage contains a cycle")
		}
		seen[key] = struct{}{}
		if previous.SourceFileID == targetSourceFileID {
			return true, nil
		}
		current = previous
	}
	return false, nil
}

func casSupersedeBaseGeneration(database *gorm.DB, base sourceGenerationModel, atMS int64) error {
	result := database.Model(&sourceGenerationModel{}).
		Where(
			"source_file_id = ? AND generation = ? AND state = ? AND fingerprint_sha256 = ?",
			base.SourceFileID, base.Generation, string(GenerationActive), base.FingerprintSHA256,
		).
		Updates(map[string]any{"state": string(GenerationSuperseded), "updated_at_ms": atMS})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return invalidRecord("replacement base generation compare-and-swap failed")
	}
	return nil
}

func casSupersedeBuildingGeneration(
	database *gorm.DB,
	building sourceGenerationModel,
	atMS int64,
) error {
	result := database.Model(&sourceGenerationModel{}).
		Where(
			"source_file_id = ? AND generation = ? AND state = ? AND fingerprint_sha256 = ? AND parser_version = ?",
			building.SourceFileID, building.Generation, string(GenerationBuilding),
			building.FingerprintSHA256, building.ParserVersion,
		).
		Updates(map[string]any{"state": string(GenerationSuperseded), "updated_at_ms": atMS})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return invalidRecord("building generation compare-and-swap failed")
	}
	return database.Where(
		"source_file_id = ? AND generation = ?", building.SourceFileID, building.Generation,
	).Delete(&sourceGenerationBatchModel{}).Error
}

func (unit *WriteUnit) supersedeDependentBuildingGenerations(
	database *gorm.DB,
	base sourceGenerationModel,
	winner *sourceGenerationModel,
	atMS int64,
) error {
	var buildings []sourceGenerationModel
	query := database.Where(
		"state = ? AND base_source_file_id = ? AND base_generation = ? AND base_fingerprint_sha256 = ?",
		string(GenerationBuilding), base.SourceFileID, base.Generation, base.FingerprintSHA256,
	)
	if winner != nil {
		query = query.Where(
			"NOT (source_file_id = ? AND generation = ?)", winner.SourceFileID, winner.Generation,
		)
	}
	if err := query.Order("source_file_id").Order("generation").Find(&buildings).Error; err != nil {
		return err
	}
	winnerSourceFileID := base.SourceFileID
	if winner != nil {
		winnerSourceFileID = winner.SourceFileID
	}
	return unit.supersedeBuildingSet(database, buildings, winnerSourceFileID, atMS)
}

func (unit *WriteUnit) supersedeInitialSiblingGenerations(
	database *gorm.DB,
	winner sourceGenerationModel,
	atMS int64,
) error {
	var siblings []sourceGenerationModel
	if err := database.Where(
		"state = ? AND current_path = ? AND base_source_file_id IS NULL AND base_generation IS NULL AND base_fingerprint_sha256 IS NULL",
		string(GenerationBuilding), winner.CurrentPath,
	).Where(
		"NOT (source_file_id = ? AND generation = ?)", winner.SourceFileID, winner.Generation,
	).Order("source_file_id").Order("generation").Find(&siblings).Error; err != nil {
		return err
	}
	return unit.supersedeBuildingSet(database, siblings, winner.SourceFileID, atMS)
}

func (unit *WriteUnit) supersedeBuildingSet(
	database *gorm.DB,
	buildings []sourceGenerationModel,
	winnerSourceFileID string,
	atMS int64,
) error {
	for _, building := range buildings {
		if err := casSupersedeBuildingGeneration(database, building, atMS); err != nil {
			return err
		}
		if building.SourceFileID == winnerSourceFileID {
			continue
		}
		if err := unit.markSourceFileUnavailable(database, building.SourceFileID, atMS); err != nil {
			return err
		}
	}
	return nil
}

func (unit *WriteUnit) markSourceFileUnavailable(
	database *gorm.DB,
	sourceFileID string,
	atMS int64,
) error {
	file, found, err := sourceFileByID(unit.ctx, database, sourceFileID)
	if err != nil {
		return err
	}
	if !found {
		return invalidRecord("superseded building source file is unavailable")
	}
	file.State = SourceFileUnavailable
	file.LastScannedAtMS = &atMS
	file.UpdatedAtMS = atMS
	return unit.UpsertSourceFile(file)
}

func generationByID(
	ctx context.Context,
	database *gorm.DB,
	sourceFileID string,
	generation int64,
) (sourceGenerationModel, bool, error) {
	var model sourceGenerationModel
	err := database.WithContext(ctx).
		Where("source_file_id = ? AND generation = ?", sourceFileID, generation).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sourceGenerationModel{}, false, nil
	}
	return model, err == nil, err
}

func (unit *WriteUnit) baseGenerationSessionID(
	database *gorm.DB,
	base sourceGenerationModel,
	hasBase bool,
) (*string, error) {
	if !hasBase {
		return nil, nil
	}
	if base.SessionID != nil {
		return base.SessionID, nil
	}
	file, found, err := sourceFileByID(unit.ctx, database, base.SourceFileID)
	if err != nil || !found {
		return nil, err
	}
	return file.SessionID, nil
}

func ensureExclusiveActiveSessionOwner(
	database *gorm.DB,
	base sourceGenerationModel,
	sessionID string,
) error {
	var count int64
	err := database.Model(&sourceGenerationModel{}).
		Where("state = ? AND session_id = ?", string(GenerationActive), sessionID).
		Where("NOT (source_file_id = ? AND generation = ?)", base.SourceFileID, base.Generation).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count != 0 {
		return invalidRecord("session belongs to another active source generation")
	}
	return nil
}

func (unit *WriteUnit) advanceActiveSourceFile(batch IngestBatch) error {
	file, found, err := sourceFileByID(unit.ctx, unit.transaction, batch.SourceFileID)
	if err != nil {
		return err
	}
	if !found {
		return invalidRecord("ingest source file does not exist")
	}
	sessionID := checkpointSessionID(batch.Checkpoint)
	if sessionID == "" {
		file.SessionID = nil
	} else {
		file.SessionID = &sessionID
	}
	if err := unit.transaction.WithContext(unit.ctx).Model(&sourceGenerationModel{}).
		Where("source_file_id = ? AND generation = ?", batch.SourceFileID, batch.Generation).
		Update("session_id", nullableNonEmptyString(sessionID)).Error; err != nil {
		return err
	}
	file.CurrentPath = batch.Fingerprint.CurrentPath
	file.SizeBytes = batch.Fingerprint.SizeBytes
	file.MTimeNS = batch.Fingerprint.MTimeNS
	file.ParsedOffset = batch.Checkpoint.CommittedOffset
	file.ParserVersion = batch.Checkpoint.ParserVersion
	file.ActiveGeneration = batch.Generation
	file.State = SourceFileActive
	file.LastScannedAtMS = &batch.AtMS
	file.LastErrorClass = nil
	file.UpdatedAtMS = batch.AtMS
	return unit.UpsertSourceFile(file)
}

func (unit *WriteUnit) supersedeReplacedSource(batch IngestBatch) error {
	var generation sourceGenerationModel
	database := unit.transaction.WithContext(unit.ctx)
	if err := database.Where(
		"source_file_id = ? AND generation = ?", batch.SourceFileID, batch.Generation,
	).Take(&generation).Error; err != nil {
		return err
	}
	seenGenerations := make(map[string]struct{})
	seenSources := make(map[string]struct{})
	current := generation
	for {
		key := current.SourceFileID + ":" + fmt.Sprintf("%d", current.Generation)
		if _, exists := seenGenerations[key]; exists {
			return invalidRecord("superseded building lineage contains a cycle")
		}
		seenGenerations[key] = struct{}{}
		if current.ReplacesSourceFileID != nil {
			seenSources[*current.ReplacesSourceFileID] = struct{}{}
		}
		if !hasSupersededBuildingLineage(current) {
			break
		}
		previous, err := validatedSupersededBuilding(unit.ctx, database, current)
		if err != nil {
			return err
		}
		current = previous
	}
	delete(seenSources, batch.SourceFileID)
	sourceFileIDs := make([]string, 0, len(seenSources))
	for sourceFileID := range seenSources {
		sourceFileIDs = append(sourceFileIDs, sourceFileID)
	}
	slices.Sort(sourceFileIDs)
	for _, sourceFileID := range sourceFileIDs {
		file, found, err := sourceFileByID(unit.ctx, database, sourceFileID)
		if err != nil {
			return err
		}
		if !found {
			return invalidRecord("replaced source file is unavailable")
		}
		file.State = SourceFileUnavailable
		file.LastScannedAtMS = &batch.AtMS
		file.UpdatedAtMS = batch.AtMS
		if err := unit.UpsertSourceFile(file); err != nil {
			return err
		}
	}
	return nil
}

func (repository *Repository) unmarshalIngestFacts(content []byte) ([]FactBatch, error) {
	if len(content) == 0 || len(content) > maxParserCheckpointBytes {
		return nil, invalidRecord("stored ingest facts size is invalid")
	}
	var facts []FactBatch
	if err := decodeCheckpointJSON(content, &facts); err != nil {
		return nil, err
	}
	for _, fact := range facts {
		if err := repository.validateBatch(fact); err != nil {
			return nil, err
		}
	}
	return facts, nil
}

func createIngestDiagnostics(database *gorm.DB, batch IngestBatch) error {
	if len(batch.Diagnostics) == 0 {
		return nil
	}
	models := make([]parserDiagnosticModel, 0, len(batch.Diagnostics))
	for ordinal, diagnostic := range batch.Diagnostics {
		models = append(models, parserDiagnosticModel{
			SourceFileID: batch.SourceFileID, Generation: batch.Generation,
			BatchEndOffset:      batch.Checkpoint.CommittedOffset,
			BatchIdentitySHA256: ingestBatchIdentityDigest(batch), Ordinal: int64(ordinal),
			Class: diagnostic.Class, Code: diagnostic.Code, StartOffset: diagnostic.StartOffset,
			EndOffset: diagnostic.EndOffset, Retryable: diagnostic.Retryable,
		})
	}
	return database.Create(&models).Error
}

func ingestDiagnosticsForBatch(
	ctx context.Context,
	database *gorm.DB,
	sourceFileID string,
	generation int64,
	batchEndOffset int64,
	batchIdentitySHA256 string,
) ([]IngestDiagnostic, error) {
	var models []parserDiagnosticModel
	if err := database.WithContext(ctx).Where(
		"source_file_id = ? AND generation = ? AND batch_end_offset = ? AND batch_identity_sha256 = ?",
		sourceFileID, generation, batchEndOffset, batchIdentitySHA256,
	).Order("ordinal").Find(&models).Error; err != nil {
		return nil, err
	}
	diagnostics := make([]IngestDiagnostic, 0, len(models))
	for _, model := range models {
		diagnostics = append(diagnostics, IngestDiagnostic{
			Class: model.Class, Code: model.Code, StartOffset: model.StartOffset,
			EndOffset: model.EndOffset, Retryable: model.Retryable,
		})
	}
	return diagnostics, nil
}

func sourceGenerationProgressUpdates(batch IngestBatch) map[string]any {
	return map[string]any{
		"source_kind":  batch.Fingerprint.SourceKind,
		"current_path": batch.Fingerprint.CurrentPath, "size_bytes": batch.Fingerprint.SizeBytes,
		"mtime_ns": batch.Fingerprint.MTimeNS, "prefix_bytes": batch.Fingerprint.PrefixBytes,
		"prefix_sha256":      batch.Fingerprint.PrefixSHA256,
		"fingerprint_sha256": batch.Fingerprint.FingerprintSHA256,
		"committed_offset":   batch.Checkpoint.CommittedOffset, "updated_at_ms": batch.AtMS,
	}
}

func checkpointSessionID(checkpoint ParserCheckpoint) string {
	if checkpoint.Seed == nil || checkpoint.Seed.Session == nil {
		return ""
	}
	return checkpoint.Seed.Session.SessionID
}

func nullableNonEmptyString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func validIngestDiagnostic(value IngestDiagnostic, committedOffset int64) bool {
	if value.StartOffset < 0 || value.EndOffset <= value.StartOffset || value.EndOffset > committedOffset {
		return false
	}
	switch value.Class {
	case "framing", "syntax", "compatibility", "lifecycle":
	default:
		return false
	}
	switch value.Code {
	case "empty_line", "invalid_utf8", "line_too_long", "bad_json", "duplicate_json_key",
		"invalid_timestamp", "invalid_field", "unknown_rollout_type", "unknown_event_type",
		"missing_session_meta", "missing_turn_start", "ambiguous_turn", "invalid_transition",
		"orphan_turn_usage", "state_limit_exceeded":
		return true
	default:
		return false
	}
}
