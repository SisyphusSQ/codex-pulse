package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"path/filepath"

	"gorm.io/gorm"
)

func (repository *Repository) PrepareGeneration(
	ctx context.Context,
	request PrepareGenerationRequest,
) (GenerationCursor, error) {
	if repository == nil || repository.database == nil {
		return GenerationCursor{}, ErrInvalidRepository
	}
	if err := validatePrepareGenerationRequest(request); err != nil {
		return GenerationCursor{}, err
	}
	var cursor GenerationCursor
	err := repository.WithinWriteUnit(ctx, func(unit *WriteUnit) error {
		var err error
		cursor, err = unit.prepareGeneration(request)
		return err
	})
	return cursor, err
}

func (repository *Repository) GenerationCursor(
	ctx context.Context,
	sourceFileID string,
	generation int64,
) (GenerationCursor, error) {
	if repository == nil || repository.database == nil {
		return GenerationCursor{}, ErrInvalidRepository
	}
	if sourceFileID == "" || generation < 0 {
		return GenerationCursor{}, invalidRecord("generation cursor identity is invalid")
	}
	var cursor GenerationCursor
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		var found bool
		var err error
		cursor, found, err = generationCursorByID(ctx, connection, sourceFileID, generation)
		if err == nil && !found {
			return ErrNotFound
		}
		return err
	})
	return cursor, err
}

// BuildingGenerationCursor 返回指定 source 当前唯一的 building generation。
func (repository *Repository) BuildingGenerationCursor(
	ctx context.Context,
	sourceFileID string,
) (GenerationCursor, error) {
	if repository == nil || repository.database == nil {
		return GenerationCursor{}, ErrInvalidRepository
	}
	if sourceFileID == "" {
		return GenerationCursor{}, invalidRecord("building generation source identity is invalid")
	}
	var cursor GenerationCursor
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		building, found, err := generationByState(ctx, connection, sourceFileID, GenerationBuilding)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		cursor, _, err = generationCursorByID(ctx, connection, sourceFileID, building.Generation)
		return err
	})
	return cursor, err
}

// BuildingGenerationCursors 返回全部尚未激活的 generation，供 Indexer 在仅持有
// durable active snapshots 的进程恢复中重建 building CAS 上下文。
func (repository *Repository) BuildingGenerationCursors(ctx context.Context) ([]GenerationCursor, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	var cursors []GenerationCursor
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		var models []sourceGenerationModel
		if err := connection.WithContext(ctx).
			Where("state = ?", string(GenerationBuilding)).
			Order("updated_at_ms DESC").Order("source_file_id").Order("generation DESC").
			Find(&models).Error; err != nil {
			return err
		}
		cursors = make([]GenerationCursor, 0, len(models))
		for _, model := range models {
			cursor, found, err := generationCursorByID(ctx, connection, model.SourceFileID, model.Generation)
			if err != nil {
				return err
			}
			if !found {
				return invalidRecord("building generation disappeared during readback")
			}
			cursors = append(cursors, cursor)
		}
		return nil
	})
	return cursors, err
}

// CodexSnapshots 返回已经原子激活的 Codex source fingerprints。
// building generation 不进入 discovery previous truth，避免半成品重建遮蔽旧事实。
func (repository *Repository) CodexSnapshots(ctx context.Context) ([]SourceFingerprint, error) {
	if repository == nil || repository.database == nil {
		return nil, ErrInvalidRepository
	}
	var snapshots []SourceFingerprint
	err := repository.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		var models []sourceGenerationModel
		if err := connection.WithContext(ctx).
			Where("provider = ? AND state = ?", "codex", string(GenerationActive)).
			Order("current_path").Order("source_file_id").Find(&models).Error; err != nil {
			return err
		}
		snapshots = make([]SourceFingerprint, 0, len(models))
		for _, model := range models {
			snapshots = append(snapshots, sourceFingerprintFromGeneration(model))
		}
		return nil
	})
	return snapshots, err
}

func (unit *WriteUnit) prepareGeneration(request PrepareGenerationRequest) (GenerationCursor, error) {
	if err := unit.requireActive(); err != nil {
		return GenerationCursor{}, err
	}
	database := unit.transaction.WithContext(unit.ctx)
	current := request.Current

	building, hasBuilding, err := unit.buildingCandidateForPrepare(database, request)
	if err != nil {
		return GenerationCursor{}, err
	}
	var base *GenerationBase
	var supersededBuilding *BuildingGenerationExpectation
	if hasBuilding {
		if building.SourceFileID == current.SourceFileID &&
			building.ParserVersion == request.ParserVersion &&
			sourceFingerprintEqual(sourceFingerprintFromGeneration(building), current) &&
			(equalStringPointer(building.ReplacesSourceFileID, request.ReplacesSourceFileID) ||
				request.ReplacesSourceFileID == nil && building.ReplacesSourceFileID != nil) {
			cursor, _, err := generationCursorByID(unit.ctx, database, current.SourceFileID, building.Generation)
			return cursor, err
		}
		base, supersededBuilding, err = unit.supersedeBuildingForPrepare(database, request, building)
		if err != nil {
			return GenerationCursor{}, err
		}
		if request.ReplacesSourceFileID == nil && building.ReplacesSourceFileID != nil {
			replacedSourceFileID := *building.ReplacesSourceFileID
			request.ReplacesSourceFileID = &replacedSourceFileID
		}
	} else {
		if request.SupersedeBuilding != nil {
			return GenerationCursor{}, invalidRecord("building generation compare-and-swap target is unavailable")
		}
		active, hasActive, err := generationByState(unit.ctx, database, current.SourceFileID, GenerationActive)
		if err != nil {
			return GenerationCursor{}, err
		}
		if request.Mode == GenerationModeAppend {
			if !hasActive || request.Previous == nil || active.ParserVersion != request.ParserVersion ||
				!sourceFingerprintEqual(sourceFingerprintFromGeneration(active), *request.Previous) ||
				request.Previous.SourceFileID != current.SourceFileID ||
				current.DeviceID != request.Previous.DeviceID || current.Inode != request.Previous.Inode ||
				current.SizeBytes < active.CommittedOffset {
				return GenerationCursor{}, invalidRecord("append generation has no matching active checkpoint")
			}
			cursor, _, err := generationCursorByID(unit.ctx, database, current.SourceFileID, active.Generation)
			if err == nil {
				cursor.Fingerprint = current
			}
			return cursor, err
		}

		base, err = unit.validateGenerationPrevious(request, active, hasActive)
		if err != nil {
			return GenerationCursor{}, err
		}
	}
	file, hasFile, err := sourceFileByID(unit.ctx, database, current.SourceFileID)
	if err != nil {
		return GenerationCursor{}, err
	}
	generation := int64(0)
	if hasFile {
		if file.Provider != current.Provider || file.DeviceID != current.DeviceID || file.Inode != current.Inode {
			return GenerationCursor{}, invalidRecord("source fingerprint conflicts with stable source identity")
		}
		generation = file.ActiveGeneration + 1
		var latest int64
		if err := database.Model(&sourceGenerationModel{}).
			Where("source_file_id = ?", current.SourceFileID).
			Select("COALESCE(MAX(generation), -1)").Scan(&latest).Error; err != nil {
			return GenerationCursor{}, err
		}
		if latest >= generation {
			generation = latest + 1
		}
	} else {
		file = SourceFile{
			SourceFileID: current.SourceFileID, Provider: current.Provider,
			CurrentPath: current.CurrentPath, DeviceID: current.DeviceID, Inode: current.Inode,
			SizeBytes: current.SizeBytes, MTimeNS: current.MTimeNS, ParsedOffset: 0,
			ParserVersion: request.ParserVersion, ActiveGeneration: 0,
			State: SourceFileDiscovered, UpdatedAtMS: request.AtMS,
		}
		if err := unit.UpsertSourceFile(file); err != nil {
			return GenerationCursor{}, err
		}
	}
	if request.ReplacesSourceFileID != nil {
		if err := requireStoredReference(
			unit.ctx, database, &sourceFileModel{}, "source_file_id = ?",
			*request.ReplacesSourceFileID, "replaced source file",
		); err != nil {
			return GenerationCursor{}, err
		}
	}
	model := sourceGenerationModelFromFingerprint(
		current, generation, GenerationBuilding, request.ParserVersion,
		request.ReplacesSourceFileID, base, supersededBuilding, request.AtMS,
	)
	if err := database.Create(&model).Error; err != nil {
		return GenerationCursor{}, err
	}
	checkpoint := ParserCheckpoint{
		Version: ParserCheckpointVersion, ParserVersion: request.ParserVersion,
		CommittedOffset: 0,
	}
	seed, projector, err := marshalParserCheckpoint(checkpoint)
	if err != nil {
		return GenerationCursor{}, err
	}
	checkpointModel := parserCheckpointModel{
		SourceFileID: current.SourceFileID, Generation: generation,
		CheckpointVersion: checkpoint.Version, ParserSeed: seed, ProjectorState: projector,
		UpdatedAtMS: request.AtMS,
	}
	if err := database.Create(&checkpointModel).Error; err != nil {
		return GenerationCursor{}, err
	}
	return generationCursorFromModels(model, checkpointModel)
}

func (unit *WriteUnit) buildingCandidateForPrepare(
	database *gorm.DB,
	request PrepareGenerationRequest,
) (sourceGenerationModel, bool, error) {
	current, hasCurrent, err := generationByState(
		unit.ctx, database, request.Current.SourceFileID, GenerationBuilding,
	)
	if err != nil {
		return sourceGenerationModel{}, false, err
	}
	if request.ReplacesSourceFileID == nil || *request.ReplacesSourceFileID == request.Current.SourceFileID {
		return current, hasCurrent, nil
	}
	replaced, hasReplaced, err := generationByState(
		unit.ctx, database, *request.ReplacesSourceFileID, GenerationBuilding,
	)
	if err != nil {
		return sourceGenerationModel{}, false, err
	}
	if hasCurrent && hasReplaced {
		return sourceGenerationModel{}, false, invalidRecord("multiple building generations conflict with prepare request")
	}
	if hasCurrent {
		return current, true, nil
	}
	return replaced, hasReplaced, nil
}

func (unit *WriteUnit) supersedeBuildingForPrepare(
	database *gorm.DB,
	request PrepareGenerationRequest,
	building sourceGenerationModel,
) (*GenerationBase, *BuildingGenerationExpectation, error) {
	expectation := request.SupersedeBuilding
	if expectation == nil || !buildingExpectationMatchesModel(*expectation, building) {
		return nil, nil, invalidRecord("building generation compare-and-swap failed")
	}
	if request.Previous == nil ||
		!sourceFingerprintEqual(sourceFingerprintFromGeneration(building), *request.Previous) {
		return nil, nil, invalidRecord("building generation previous fingerprint is stale")
	}
	if building.SourceFileID != request.Current.SourceFileID {
		if request.ReplacesSourceFileID == nil || *request.ReplacesSourceFileID != building.SourceFileID {
			return nil, nil, invalidRecord("building replacement lineage is invalid")
		}
	} else if request.ReplacesSourceFileID != nil &&
		!equalStringPointer(building.ReplacesSourceFileID, request.ReplacesSourceFileID) {
		return nil, nil, invalidRecord("same-source building replacement lineage changed")
	}
	base := generationBaseFromModel(building)
	if base != nil {
		stored, found, err := generationByID(unit.ctx, database, base.SourceFileID, base.Generation)
		if err != nil {
			return nil, nil, err
		}
		if !found || GenerationState(stored.State) != GenerationActive ||
			stored.FingerprintSHA256 != base.FingerprintSHA256 {
			return nil, nil, invalidRecord("building generation base is stale")
		}
	}
	if err := casSupersedeBuildingGeneration(database, building, request.AtMS); err != nil {
		return nil, nil, err
	}
	copy := *expectation
	return base, &copy, nil
}

func buildingExpectationMatchesModel(
	expectation BuildingGenerationExpectation,
	model sourceGenerationModel,
) bool {
	return expectation.SourceFileID == model.SourceFileID && expectation.Generation == model.Generation &&
		expectation.FingerprintSHA256 == model.FingerprintSHA256 &&
		expectation.ParserVersion == model.ParserVersion
}

func (unit *WriteUnit) validateGenerationPrevious(
	request PrepareGenerationRequest,
	active sourceGenerationModel,
	hasActive bool,
) (*GenerationBase, error) {
	if request.Previous == nil {
		if request.ReplacesSourceFileID != nil || hasActive {
			return nil, invalidRecord("replacement generation requires previous fingerprint")
		}
		return nil, nil
	}
	previousSourceID := request.Current.SourceFileID
	if request.ReplacesSourceFileID != nil {
		previousSourceID = *request.ReplacesSourceFileID
	}
	if request.Previous.SourceFileID != previousSourceID {
		return nil, invalidRecord("previous fingerprint does not match replacement lineage")
	}
	if previousSourceID == request.Current.SourceFileID {
		if !hasActive || !sourceFingerprintEqual(sourceFingerprintFromGeneration(active), *request.Previous) {
			return nil, invalidRecord("previous fingerprint is stale")
		}
		return generationBaseFromActive(active), nil
	}
	previousActive, found, err := generationByState(unit.ctx, unit.transaction, previousSourceID, GenerationActive)
	if err != nil {
		return nil, err
	}
	if !found || !sourceFingerprintEqual(sourceFingerprintFromGeneration(previousActive), *request.Previous) {
		return nil, invalidRecord("replacement previous fingerprint is stale")
	}
	return generationBaseFromActive(previousActive), nil
}

func generationBaseFromActive(active sourceGenerationModel) *GenerationBase {
	return &GenerationBase{
		SourceFileID: active.SourceFileID, Generation: active.Generation,
		FingerprintSHA256: active.FingerprintSHA256,
	}
}

func generationByState(
	ctx context.Context,
	database *gorm.DB,
	sourceFileID string,
	state GenerationState,
) (sourceGenerationModel, bool, error) {
	var model sourceGenerationModel
	err := database.WithContext(ctx).Where("source_file_id = ? AND state = ?", sourceFileID, string(state)).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sourceGenerationModel{}, false, nil
	}
	return model, err == nil, err
}

func generationCursorByID(
	ctx context.Context,
	database *gorm.DB,
	sourceFileID string,
	generation int64,
) (GenerationCursor, bool, error) {
	var model sourceGenerationModel
	err := database.WithContext(ctx).
		Where("source_file_id = ? AND generation = ?", sourceFileID, generation).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return GenerationCursor{}, false, nil
	}
	if err != nil {
		return GenerationCursor{}, false, err
	}
	var checkpoint parserCheckpointModel
	if err := database.WithContext(ctx).
		Where("source_file_id = ? AND generation = ?", sourceFileID, generation).
		Take(&checkpoint).Error; err != nil {
		return GenerationCursor{}, false, err
	}
	cursor, err := generationCursorFromModels(model, checkpoint)
	return cursor, err == nil, err
}

func generationCursorFromModels(
	generation sourceGenerationModel,
	checkpoint parserCheckpointModel,
) (GenerationCursor, error) {
	decoded, err := unmarshalParserCheckpoint(
		checkpoint.CheckpointVersion, generation.ParserVersion, generation.CommittedOffset,
		checkpoint.ParserSeed, checkpoint.ProjectorState,
	)
	if err != nil {
		return GenerationCursor{}, err
	}
	return GenerationCursor{
		SourceFileID: generation.SourceFileID, Generation: generation.Generation,
		State: GenerationState(generation.State), Fingerprint: sourceFingerprintFromGeneration(generation),
		ParserVersion: generation.ParserVersion, ReplacesSourceFileID: generation.ReplacesSourceFileID,
		Base:               generationBaseFromModel(generation),
		SupersededBuilding: supersededBuildingFromModel(generation), Checkpoint: decoded,
	}, nil
}

func supersededBuildingFromModel(model sourceGenerationModel) *BuildingGenerationExpectation {
	if model.SupersededBuildingSourceFileID == nil || model.SupersededBuildingGeneration == nil ||
		model.SupersededBuildingFingerprintSHA256 == nil || model.SupersededBuildingParserVersion == nil {
		return nil
	}
	return &BuildingGenerationExpectation{
		SourceFileID:      *model.SupersededBuildingSourceFileID,
		Generation:        *model.SupersededBuildingGeneration,
		FingerprintSHA256: *model.SupersededBuildingFingerprintSHA256,
		ParserVersion:     *model.SupersededBuildingParserVersion,
	}
}

func generationBaseFromModel(model sourceGenerationModel) *GenerationBase {
	if model.BaseSourceFileID == nil || model.BaseGeneration == nil || model.BaseFingerprintSHA256 == nil {
		return nil
	}
	return &GenerationBase{
		SourceFileID: *model.BaseSourceFileID, Generation: *model.BaseGeneration,
		FingerprintSHA256: *model.BaseFingerprintSHA256,
	}
}

func sourceGenerationModelFromFingerprint(
	fingerprint SourceFingerprint,
	generation int64,
	state GenerationState,
	parserVersion string,
	replacesSourceFileID *string,
	base *GenerationBase,
	supersededBuilding *BuildingGenerationExpectation,
	atMS int64,
) sourceGenerationModel {
	model := sourceGenerationModel{
		SourceFileID: fingerprint.SourceFileID, Generation: generation, State: string(state),
		Provider: fingerprint.Provider, SourceKind: fingerprint.SourceKind,
		CurrentPath: fingerprint.CurrentPath, DeviceID: fingerprint.DeviceID, Inode: fingerprint.Inode,
		SizeBytes: fingerprint.SizeBytes, MTimeNS: fingerprint.MTimeNS,
		PrefixBytes: fingerprint.PrefixBytes, PrefixSHA256: fingerprint.PrefixSHA256,
		FingerprintSHA256: fingerprint.FingerprintSHA256, ParserVersion: parserVersion,
		CommittedOffset: 0, ReplacesSourceFileID: replacesSourceFileID, UpdatedAtMS: atMS,
	}
	if base != nil {
		model.BaseSourceFileID = &base.SourceFileID
		model.BaseGeneration = &base.Generation
		model.BaseFingerprintSHA256 = &base.FingerprintSHA256
	}
	if supersededBuilding != nil {
		model.SupersededBuildingSourceFileID = &supersededBuilding.SourceFileID
		model.SupersededBuildingGeneration = &supersededBuilding.Generation
		model.SupersededBuildingFingerprintSHA256 = &supersededBuilding.FingerprintSHA256
		model.SupersededBuildingParserVersion = &supersededBuilding.ParserVersion
	}
	return model
}

func sourceFingerprintFromGeneration(model sourceGenerationModel) SourceFingerprint {
	return SourceFingerprint{
		SourceFileID: model.SourceFileID, Provider: model.Provider, SourceKind: model.SourceKind,
		CurrentPath: model.CurrentPath, DeviceID: model.DeviceID, Inode: model.Inode,
		SizeBytes: model.SizeBytes, MTimeNS: model.MTimeNS, PrefixBytes: model.PrefixBytes,
		PrefixSHA256: model.PrefixSHA256, FingerprintSHA256: model.FingerprintSHA256,
	}
}

func validatePrepareGenerationRequest(request PrepareGenerationRequest) error {
	if request.Mode != GenerationModeAppend && request.Mode != GenerationModeRebuild {
		return invalidRecord("generation mode is invalid")
	}
	if request.AtMS < 0 || !validCheckpointText(request.ParserVersion, maxCheckpointIdentifier, true) ||
		!validSourceFingerprint(request.Current) {
		return invalidRecord("generation request is invalid")
	}
	if request.Previous != nil && !validSourceFingerprint(*request.Previous) {
		return invalidRecord("previous source fingerprint is invalid")
	}
	if request.ReplacesSourceFileID != nil && *request.ReplacesSourceFileID == "" {
		return invalidRecord("replacement source file ID is invalid")
	}
	if request.SupersedeBuilding != nil {
		expectation := request.SupersedeBuilding
		if expectation.SourceFileID == "" || expectation.Generation < 0 ||
			!validSHA256String(expectation.FingerprintSHA256) ||
			!validCheckpointText(expectation.ParserVersion, maxCheckpointIdentifier, true) {
			return invalidRecord("building generation expectation is invalid")
		}
	}
	return nil
}

func validSourceFingerprint(value SourceFingerprint) bool {
	if value.SourceFileID == "" || value.Provider != "codex" ||
		(value.SourceKind != "session" && value.SourceKind != "archived_session") ||
		!filepath.IsAbs(value.CurrentPath) || filepath.Clean(value.CurrentPath) != value.CurrentPath ||
		value.DeviceID == "" || value.Inode < 0 || value.SizeBytes < 0 || value.MTimeNS < 0 ||
		value.PrefixBytes < 0 || value.PrefixBytes > value.SizeBytes || value.PrefixBytes > 4096 ||
		!validSHA256String(value.PrefixSHA256) || !validSHA256String(value.FingerprintSHA256) {
		return false
	}
	return sourceFingerprintDigest(value) == value.FingerprintSHA256
}

func sourceFingerprintEqual(left, right SourceFingerprint) bool {
	return left == right
}

func sourceFingerprintDigest(value SourceFingerprint) string {
	hasher := sha256.New()
	writeFingerprintString(hasher, value.DeviceID)
	writeFingerprintInt64(hasher, value.Inode)
	writeFingerprintInt64(hasher, value.SizeBytes)
	writeFingerprintInt64(hasher, value.MTimeNS)
	writeFingerprintInt64(hasher, value.PrefixBytes)
	writeFingerprintString(hasher, value.PrefixSHA256)
	return hex.EncodeToString(hasher.Sum(nil))
}

// sourceTargetIdentityDigest 绑定一次 commit 的完整目标 snapshot；它在物理
// fingerprint 之外纳入 path/source kind，避免 metadata-only move 共用 receipt。
func sourceTargetIdentityDigest(value SourceFingerprint) string {
	hasher := sha256.New()
	writeFingerprintString(hasher, value.SourceFileID)
	writeFingerprintString(hasher, value.Provider)
	writeFingerprintString(hasher, value.SourceKind)
	writeFingerprintString(hasher, value.CurrentPath)
	writeFingerprintString(hasher, value.DeviceID)
	writeFingerprintInt64(hasher, value.Inode)
	writeFingerprintInt64(hasher, value.SizeBytes)
	writeFingerprintInt64(hasher, value.MTimeNS)
	writeFingerprintInt64(hasher, value.PrefixBytes)
	writeFingerprintString(hasher, value.PrefixSHA256)
	writeFingerprintString(hasher, value.FingerprintSHA256)
	return hex.EncodeToString(hasher.Sum(nil))
}

// ingestBatchIdentityDigest 在完整 target identity 上加入严格单调的 commit
// 时间，区分 A→B→A 这类返回同一 snapshot 的不同访问 epoch。
func ingestBatchIdentityDigest(batch IngestBatch) string {
	hasher := sha256.New()
	writeFingerprintString(hasher, sourceTargetIdentityDigest(batch.Fingerprint))
	writeFingerprintInt64(hasher, batch.AtMS)
	return hex.EncodeToString(hasher.Sum(nil))
}

func writeFingerprintString(hasher hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = hasher.Write(length[:])
	_, _ = hasher.Write([]byte(value))
}

func writeFingerprintInt64(hasher hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = hasher.Write(encoded[:])
}

func validSHA256String(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}
