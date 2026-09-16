package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	storeschema "github.com/SisyphusSQ/codex-pulse/internal/store/schema"
)

func migrateCodexSubscriptionAccountsForV33(ctx context.Context, transaction *gorm.DB) error {
	if transaction == nil {
		return fmt.Errorf("%w: invalid subscription accounts migration database", ErrMigrationContract)
	}
	if err := storeschema.EnsureObjects(ctx, transaction, codexSubscriptionSchemaObjects); err != nil {
		return err
	}
	if err := backfillDetectedCodexSubscriptionAccounts(ctx, transaction); err != nil {
		return err
	}
	if err := readbackCodexSubscriptionSchema(ctx, transaction); err != nil {
		return err
	}
	return verifyNoForeignKeyViolations(ctx, transaction)
}

func backfillDetectedCodexSubscriptionAccounts(ctx context.Context, transaction *gorm.DB) error {
	var scopes []codexAccountScopeModel
	if err := transaction.WithContext(ctx).Order("account_scope").Find(&scopes).Error; err != nil {
		return fmt.Errorf("%w: list account scopes: %v", ErrMigrationContract, err)
	}
	for _, scope := range scopes {
		if err := ensureDetectedCodexSubscriptionAccount(ctx, transaction, scope.AccountScope); err != nil {
			return fmt.Errorf("%w: backfill detected subscription account: %v", ErrMigrationContract, err)
		}
	}
	return nil
}

func readbackCodexSubscriptionSchema(ctx context.Context, transaction *gorm.DB) error {
	for _, object := range codexSubscriptionSchemaObjects {
		exists, err := storeschema.VerifyObject(ctx, transaction, object)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: missing %s %q", ErrMigrationContract, object.ObjectType, object.Name)
		}
	}
	return nil
}

func ensureDetectedCodexSubscriptionAccount(
	ctx context.Context,
	transaction *gorm.DB,
	accountScope string,
) error {
	if !validDerivedCodexAccountScope(accountScope) {
		return invalidRecord("codex account scope is invalid")
	}
	var existing codexSubscriptionDetectedAccountModel
	err := transaction.WithContext(ctx).Where("account_scope = ?", accountScope).Take(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	publicID, err := newCodexSubscriptionPublicID()
	if err != nil {
		return err
	}
	return transaction.WithContext(ctx).Create(&codexSubscriptionDetectedAccountModel{
		AccountScope:       accountScope,
		DetectedAccountID:  publicID,
		AutomaticPlanState: subscriptionaccounts.AutomaticPlanUnavailable,
		Revision:           1,
	}).Error
}

func newCodexSubscriptionPublicID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", invalidRecord("codex subscription public id could not be generated")
	}
	return id.String(), nil
}
