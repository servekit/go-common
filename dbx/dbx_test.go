package dbx

import (
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// --- Test models ---

type amUser struct {
	ID    uint   `gorm:"primarykey"`
	Name  string
	Email string
}

type amUserProfile struct {
	ID     uint `gorm:"primarykey"`
	UserID uint
	Bio    string
}

type amProduct struct {
	ID    uint    `gorm:"primarykey"`
	Name  string
	Price float64
}

type amLegacyOrder struct {
	ID     uint    `gorm:"primarykey"`
	Amount float64
}

func (amLegacyOrder) TableName() string {
	return "orders"
}

type amTag struct {
	ID   uint   `gorm:"primarykey"`
	Name string
}

type amCategory struct {
	ID   uint   `gorm:"primarykey"`
	Name string
}

type amDetailItem struct {
	ID   uint   `gorm:"primarykey"`
	Name string `gorm:"type:varchar(100);not null"`
}

// --- Helpers ---

func dbWithPrefix(t *testing.T, base *gorm.DB, prefix string) *gorm.DB {
	t.Helper()
	sqlDB, err := base.DB()
	if err != nil {
		t.Fatalf("get underlying sql.DB: %v", err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		NamingStrategy: &schema.NamingStrategy{
			TablePrefix: prefix,
		},
	})
	if err != nil {
		t.Fatalf("open gorm with prefix %q: %v", prefix, err)
	}
	return db
}

func tableExists(t *testing.T, db *gorm.DB, tableName string) bool {
	t.Helper()
	var exists bool
	row := db.Raw(
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = ?)",
		tableName,
	).Row()
	if err := row.Scan(&exists); err != nil {
		t.Fatalf("check table %q existence: %v", tableName, err)
	}
	return exists
}

func mustTableExist(t *testing.T, db *gorm.DB, tableName string) {
	t.Helper()
	if !tableExists(t, db, tableName) {
		t.Errorf("expected table %q to exist", tableName)
	}
}

func mustTableNotExist(t *testing.T, db *gorm.DB, tableName string) {
	t.Helper()
	if tableExists(t, db, tableName) {
		t.Errorf("expected table %q to NOT exist", tableName)
	}
}

func columnType(t *testing.T, db *gorm.DB, tableName, columnName string) string {
	t.Helper()
	var dataType string
	row := db.Raw(
		"SELECT data_type FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?",
		tableName, columnName,
	).Row()
	if err := row.Scan(&dataType); err != nil {
		t.Fatalf("get column type for %q.%q: %v", tableName, columnName, err)
	}
	return dataType
}

func columnNullable(t *testing.T, db *gorm.DB, tableName, columnName string) bool {
	t.Helper()
	var nullable string
	row := db.Raw(
		"SELECT is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? AND column_name = ?",
		tableName, columnName,
	).Row()
	if err := row.Scan(&nullable); err != nil {
		t.Fatalf("check nullable for %q.%q: %v", tableName, columnName, err)
	}
	return nullable == "YES"
}

// --- Tests ---

func TestAutoMigrate(t *testing.T) {
	db := SetupTestDB(t)

	t.Run("basic pluralization", func(t *testing.T) {
		if err := AutoMigrate(db, &amUser{}); err != nil {
			t.Fatal(err)
		}
		mustTableExist(t, db, "am_users")
		mustTableNotExist(t, db, "am_user")
	})

	t.Run("snake_case composite name", func(t *testing.T) {
		if err := AutoMigrate(db, &amUserProfile{}); err != nil {
			t.Fatal(err)
		}
		mustTableExist(t, db, "am_user_profiles")
	})

	t.Run("table prefix applied with pluralization", func(t *testing.T) {
		prefixedDB := dbWithPrefix(t, db, "app_")
		if err := AutoMigrate(prefixedDB, &amProduct{}); err != nil {
			t.Fatal(err)
		}
		mustTableExist(t, db, "app_am_products")
		mustTableNotExist(t, db, "am_products")
	})

	t.Run("custom TableName overrides convention", func(t *testing.T) {
		if err := AutoMigrate(db, &amLegacyOrder{}); err != nil {
			t.Fatal(err)
		}
		mustTableExist(t, db, "orders")
		mustTableNotExist(t, db, "am_legacy_orders")
	})

	t.Run("custom TableName combined with prefix", func(t *testing.T) {
		prefixedDB := dbWithPrefix(t, db, "svc_")
		if err := AutoMigrate(prefixedDB, &amLegacyOrder{}); err != nil {
			t.Fatal(err)
		}
		mustTableExist(t, db, "svc_orders")
	})

	t.Run("multiple models in one call", func(t *testing.T) {
		if err := AutoMigrate(db, &amTag{}, &amCategory{}); err != nil {
			t.Fatal(err)
		}
		mustTableExist(t, db, "am_tags")
		mustTableExist(t, db, "am_categories")
	})

	t.Run("idempotent", func(t *testing.T) {
		// am_users already created in "basic pluralization"
		if err := AutoMigrate(db, &amUser{}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("column types and constraints", func(t *testing.T) {
		if err := AutoMigrate(db, &amDetailItem{}); err != nil {
			t.Fatal(err)
		}
		mustTableExist(t, db, "am_detail_items")

		if dt := columnType(t, db, "am_detail_items", "name"); dt != "character varying" {
			t.Errorf("expected name type %q, got %q", "character varying", dt)
		}
		if columnNullable(t, db, "am_detail_items", "name") {
			t.Error("expected name column to be NOT NULL")
		}
	})
}
