package seed

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

func lookupKey(tx db.Tx[db.Tenant], resource Resource, value string) (Key, bool, error) {
	key := Key{Value: value}
	var id *uuid.UUID
	err := tx.DB().Raw(`SELECT record_id FROM seed_keys WHERE tenant_id = ? AND module = ? AND entity = ? AND key = ?`,
		db.TenantOf(tx).ID, resource.Module, resource.Entity, value).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if resource.NaturalKey == "" {
			key.Value = ""
		}
		return key, false, nil
	}
	if err != nil {
		return Key{}, false, fmt.Errorf("read seed key: %w", err)
	}
	if id != nil {
		key.RecordID = *id
	}
	return key, true, nil
}

func putKey(tx db.Tx[db.Tenant], resource Resource, value, kind string, id uuid.UUID) error {
	var recordID any
	if id != uuid.Nil {
		recordID = id
	}
	err := tx.DB().Exec(`INSERT INTO seed_keys (tenant_id, module, entity, key, kind, record_id)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, module, entity, key)
		DO UPDATE SET kind = excluded.kind, record_id = excluded.record_id`,
		db.TenantOf(tx).ID, resource.Module, resource.Entity, value, kind, recordID).Error
	if err != nil {
		return fmt.Errorf("write seed key: %w", err)
	}
	return nil
}

func ownedKeys(tx db.Tx[db.Tenant], resource Resource, kind string) ([]Key, error) {
	rows, err := tx.DB().Raw(`SELECT key, record_id FROM seed_keys
		WHERE tenant_id = ? AND module = ? AND entity = ? AND kind = ? ORDER BY key`,
		db.TenantOf(tx).ID, resource.Module, resource.Entity, kind).Rows()
	if err != nil {
		return nil, fmt.Errorf("list seed keys: %w", err)
	}
	defer rows.Close()
	var keys []Key
	for rows.Next() {
		var key Key
		var id *uuid.UUID
		if err := rows.Scan(&key.Value, &id); err != nil {
			return nil, fmt.Errorf("scan seed key: %w", err)
		}
		if id != nil {
			key.RecordID = *id
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func deleteKey(tx db.Tx[db.Tenant], resource Resource, value string) error {
	result := tx.DB().Exec(`DELETE FROM seed_keys WHERE tenant_id = ? AND module = ? AND entity = ? AND key = ?`,
		db.TenantOf(tx).ID, resource.Module, resource.Entity, value)
	if result.Error != nil {
		return fmt.Errorf("delete seed key: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("seed key %s/%s disappeared during prune", resource.Alias, value)
	}
	return nil
}
