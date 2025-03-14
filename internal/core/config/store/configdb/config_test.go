package configdb

import (
	"context"
	"testing"

	"github.com/ixugo/goweb/pkg/orm"
	"wvp/internal/core/config"
)

func TestConfigGet(t *testing.T) {
	db, mock, err := generateMockDB()
	if err != nil {
		t.Fatal(err)
	}
	userDB := NewConfig(db)

	mock.ExpectQuery(`SELECT \* FROM "configs" WHERE id=\$1 (.+) LIMIT \$2`).WithArgs("jack", 1)
	var out config.Config
	if err := userDB.Get(context.Background(), &out, orm.Where("id=?", "jack")); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal("ExpectationsWereMet err:", err)
	}
}
