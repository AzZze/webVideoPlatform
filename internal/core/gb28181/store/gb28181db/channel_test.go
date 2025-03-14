package gb28181db

import (
	"context"
	"testing"

	"github.com/ixugo/goweb/pkg/orm"
	"wvp/internal/core/gb28181"
)

func TestChannelGet(t *testing.T) {
	db, mock, err := generateMockDB()
	if err != nil {
		t.Fatal(err)
	}
	userDB := NewChannel(db)

	mock.ExpectQuery(`SELECT \* FROM "channels" WHERE id=\$1 (.+) LIMIT \$2`).WithArgs("jack", 1)
	var out gb28181.Channel
	if err := userDB.Get(context.Background(), &out, orm.Where("id=?", "jack")); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal("ExpectationsWereMet err:", err)
	}
}
