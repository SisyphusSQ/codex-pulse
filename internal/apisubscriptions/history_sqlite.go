package apisubscriptions

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"unicode"

	"gorm.io/gorm"

	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

type SQLiteBalanceHistory struct {
	database *storesqlite.Store
}

type sqliteBalanceObservationModel struct {
	Service         string `gorm:"column:service;primaryKey"`
	CredentialEpoch string `gorm:"column:credential_epoch;primaryKey"`
	ObservedAtMS    int64  `gorm:"column:observed_at_ms;primaryKey"`
	Currency        string `gorm:"column:currency;primaryKey"`
	IsAvailable     bool   `gorm:"column:is_available"`
	Total           string `gorm:"column:total_balance"`
	Granted         string `gorm:"column:granted_balance"`
	ToppedUp        string `gorm:"column:topped_up_balance"`
}

type sqliteQuotaObservationModel struct {
	Service          string  `gorm:"column:service;primaryKey"`
	CredentialEpoch  string  `gorm:"column:credential_epoch;primaryKey"`
	ObservedAtMS     int64   `gorm:"column:observed_at_ms;primaryKey"`
	WindowKind       string  `gorm:"column:window_kind;primaryKey"`
	Status           string  `gorm:"column:status"`
	UsedPercent      float64 `gorm:"column:used_percent"`
	RemainingPercent float64 `gorm:"column:remaining_percent"`
	ResetsAtMS       int64   `gorm:"column:resets_at_ms"`
}

func (sqliteBalanceObservationModel) TableName() string {
	return "api_subscription_balance_observations"
}

func (sqliteQuotaObservationModel) TableName() string {
	return "api_subscription_quota_observations"
}

func NewSQLiteBalanceHistory(database *storesqlite.Store) (*SQLiteBalanceHistory, error) {
	if database == nil {
		return nil, errors.New("API subscription balance history database is required")
	}
	return &SQLiteBalanceHistory{database: database}, nil
}

func (history *SQLiteBalanceHistory) Record(ctx context.Context, observation BalanceObservation) error {
	if history == nil || history.database == nil || !validBalanceObservation(observation) {
		return ErrProtocol
	}
	models := make([]sqliteBalanceObservationModel, 0, len(observation.Balance.Balances))
	for _, balance := range observation.Balance.Balances {
		models = append(models, sqliteBalanceObservationModel{
			Service: ServiceDeepSeek, CredentialEpoch: observation.CredentialEpoch,
			ObservedAtMS: observation.ObservedAtMS, Currency: balance.Currency,
			IsAvailable: observation.Balance.IsAvailable,
			Total:       balance.Total, Granted: balance.Granted, ToppedUp: balance.ToppedUp,
		})
	}
	return history.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		var existing []sqliteBalanceObservationModel
		if err := database.Where(
			"service = ? AND credential_epoch = ? AND observed_at_ms = ?",
			ServiceDeepSeek, observation.CredentialEpoch, observation.ObservedAtMS,
		).Order("currency").Find(&existing).Error; err != nil {
			return fmt.Errorf("read API subscription balance replay: %w", err)
		}
		if len(existing) > 0 {
			if equalSQLiteBalanceModels(existing, models) {
				return nil
			}
			return ErrProtocol
		}
		if err := database.Create(&models).Error; err != nil {
			return fmt.Errorf("record API subscription balance observation: %w", err)
		}
		return nil
	})
}

func (history *SQLiteBalanceHistory) Observations(
	ctx context.Context,
	credentialEpoch string,
	startsAtMS int64,
	endsAtMS int64,
) ([]BalanceObservation, error) {
	if history == nil || history.database == nil || !validCredentialEpoch(credentialEpoch) ||
		startsAtMS < 0 || endsAtMS < startsAtMS {
		return nil, ErrProtocol
	}
	result := make([]BalanceObservation, 0)
	err := history.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		var rows []sqliteBalanceObservationModel
		if err := connection.WithContext(ctx).Where(
			"service = ? AND credential_epoch = ? AND observed_at_ms >= ? AND observed_at_ms <= ?",
			ServiceDeepSeek, credentialEpoch, startsAtMS, endsAtMS,
		).Order("observed_at_ms").Order("currency").Find(&rows).Error; err != nil {
			return fmt.Errorf("read API subscription balance observations: %w", err)
		}
		for _, row := range rows {
			if len(result) == 0 || result[len(result)-1].ObservedAtMS != row.ObservedAtMS {
				result = append(result, BalanceObservation{
					CredentialEpoch: credentialEpoch,
					ObservedAtMS:    row.ObservedAtMS,
					Balance: Balance{
						IsAvailable: row.IsAvailable,
						Balances:    make([]CurrencyBalance, 0),
					},
				})
			}
			observation := &result[len(result)-1]
			if observation.Balance.IsAvailable != row.IsAvailable {
				return ErrProtocol
			}
			observation.Balance.Balances = append(observation.Balance.Balances, CurrencyBalance{
				Currency: row.Currency, Total: row.Total, Granted: row.Granted, ToppedUp: row.ToppedUp,
			})
		}
		return nil
	})
	return result, err
}

func (history *SQLiteBalanceHistory) Latest(
	ctx context.Context,
	credentialEpoch string,
	endsAtMS int64,
) (BalanceObservation, bool, error) {
	if history == nil || history.database == nil || !validCredentialEpoch(credentialEpoch) || endsAtMS < 0 {
		return BalanceObservation{}, false, ErrProtocol
	}
	var result BalanceObservation
	found := false
	err := history.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		database := connection.WithContext(ctx)
		var latest sqliteBalanceObservationModel
		err := database.Where(
			"service = ? AND credential_epoch = ? AND observed_at_ms <= ?",
			ServiceDeepSeek, credentialEpoch, endsAtMS,
		).Order("observed_at_ms DESC").Order("currency").First(&latest).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read latest API subscription balance: %w", err)
		}
		var rows []sqliteBalanceObservationModel
		if err := database.Where(
			"service = ? AND credential_epoch = ? AND observed_at_ms = ?",
			ServiceDeepSeek, credentialEpoch, latest.ObservedAtMS,
		).Order("currency").Find(&rows).Error; err != nil {
			return fmt.Errorf("read latest API subscription balance currencies: %w", err)
		}
		if len(rows) == 0 {
			return ErrProtocol
		}
		result = BalanceObservation{
			CredentialEpoch: credentialEpoch,
			ObservedAtMS:    latest.ObservedAtMS,
			Balance:         Balance{IsAvailable: latest.IsAvailable, Balances: make([]CurrencyBalance, 0, len(rows))},
		}
		for _, row := range rows {
			if row.IsAvailable != latest.IsAvailable {
				return ErrProtocol
			}
			result.Balance.Balances = append(result.Balance.Balances, CurrencyBalance{
				Currency: row.Currency, Total: row.Total, Granted: row.Granted, ToppedUp: row.ToppedUp,
			})
		}
		found = true
		return nil
	})
	return result, found, err
}

func (history *SQLiteBalanceHistory) RecordQuota(ctx context.Context, observation QuotaObservation) error {
	if history == nil || history.database == nil || !validQuotaObservation(observation) {
		return ErrProtocol
	}
	models := make([]sqliteQuotaObservationModel, 0, len(observation.Quota.Windows))
	for _, window := range observation.Quota.Windows {
		models = append(models, sqliteQuotaObservationModel{
			Service: ServiceOpenCodeGo, CredentialEpoch: observation.CredentialEpoch,
			ObservedAtMS: observation.ObservedAtMS, WindowKind: window.Kind, Status: window.Status,
			UsedPercent: window.UsedPercent, RemainingPercent: window.RemainingPercent,
			ResetsAtMS: window.ResetsAtMS,
		})
	}
	return history.database.Write(ctx, func(ctx context.Context, transaction *gorm.DB) error {
		database := transaction.WithContext(ctx)
		var existing []sqliteQuotaObservationModel
		if err := database.Where(
			"service = ? AND credential_epoch = ? AND observed_at_ms = ?",
			ServiceOpenCodeGo, observation.CredentialEpoch, observation.ObservedAtMS,
		).Order("window_kind").Find(&existing).Error; err != nil {
			return fmt.Errorf("read API subscription quota replay: %w", err)
		}
		if len(existing) > 0 {
			if equalSQLiteQuotaModels(existing, models) {
				return nil
			}
			return ErrProtocol
		}
		if err := database.Create(&models).Error; err != nil {
			return fmt.Errorf("record API subscription quota observation: %w", err)
		}
		return nil
	})
}

func (history *SQLiteBalanceHistory) QuotaObservations(
	ctx context.Context,
	credentialEpoch string,
	startsAtMS int64,
	endsAtMS int64,
) ([]QuotaObservation, error) {
	if history == nil || history.database == nil || !validCredentialEpoch(credentialEpoch) ||
		startsAtMS < 0 || endsAtMS < startsAtMS {
		return nil, ErrProtocol
	}
	result := make([]QuotaObservation, 0)
	err := history.database.View(ctx, func(ctx context.Context, connection *gorm.DB) error {
		var rows []sqliteQuotaObservationModel
		if err := connection.WithContext(ctx).Where(
			"service = ? AND credential_epoch = ? AND observed_at_ms >= ? AND observed_at_ms <= ?",
			ServiceOpenCodeGo, credentialEpoch, startsAtMS, endsAtMS,
		).Order("observed_at_ms").Order("window_kind").Find(&rows).Error; err != nil {
			return fmt.Errorf("read API subscription quota observations: %w", err)
		}
		for _, row := range rows {
			if len(result) == 0 || result[len(result)-1].ObservedAtMS != row.ObservedAtMS {
				result = append(result, QuotaObservation{
					CredentialEpoch: credentialEpoch, ObservedAtMS: row.ObservedAtMS,
					Quota: Quota{Windows: make([]QuotaWindow, 0)},
				})
			}
			result[len(result)-1].Quota.Windows = append(result[len(result)-1].Quota.Windows, QuotaWindow{
				Kind: row.WindowKind, Status: row.Status, UsedPercent: row.UsedPercent,
				RemainingPercent: row.RemainingPercent, ResetsAtMS: row.ResetsAtMS,
			})
		}
		return nil
	})
	return result, err
}

func validBalanceObservation(observation BalanceObservation) bool {
	if !validCredentialEpoch(observation.CredentialEpoch) || observation.ObservedAtMS < 0 ||
		len(observation.Balance.Balances) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(observation.Balance.Balances))
	for _, balance := range observation.Balance.Balances {
		if !validCurrency(balance.Currency) || !validMoney(balance.Total) ||
			!validMoney(balance.Granted) || !validMoney(balance.ToppedUp) {
			return false
		}
		if _, exists := seen[balance.Currency]; exists {
			return false
		}
		seen[balance.Currency] = struct{}{}
	}
	return true
}

func validCredentialEpoch(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) ||
			character == '-' || character == '_' || character == '.' || character == ':' {
			continue
		}
		return false
	}
	return true
}

func equalSQLiteBalanceModels(left []sqliteBalanceObservationModel, right []sqliteBalanceObservationModel) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]sqliteBalanceObservationModel(nil), left...)
	rightCopy := append([]sqliteBalanceObservationModel(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool { return leftCopy[i].Currency < leftCopy[j].Currency })
	sort.Slice(rightCopy, func(i, j int) bool { return rightCopy[i].Currency < rightCopy[j].Currency })
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func equalSQLiteQuotaModels(left []sqliteQuotaObservationModel, right []sqliteQuotaObservationModel) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]sqliteQuotaObservationModel(nil), left...)
	rightCopy := append([]sqliteQuotaObservationModel(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool { return leftCopy[i].WindowKind < leftCopy[j].WindowKind })
	sort.Slice(rightCopy, func(i, j int) bool { return rightCopy[i].WindowKind < rightCopy[j].WindowKind })
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}
