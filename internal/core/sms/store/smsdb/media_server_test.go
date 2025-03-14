package smsdb

import (
	"context"
	"testing"

	"github.com/ixugo/goweb/pkg/orm"
	"wvp/internal/core/sms"
)

func TestMediaServerGet(t *testing.T) {
	db, mock, err := generateMockDB()
	if err != nil {
		t.Fatal(err)
	}
	userDB := NewMediaServer(db)

	mock.ExpectQuery(`SELECT \* FROM "media_servers" WHERE id=\$1 (.+) LIMIT \$2`).WithArgs("jack", 1)
	var out sms.MediaServer
	if err := userDB.Get(context.Background(), &out, orm.Where("id=?", "jack")); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal("ExpectationsWereMet err:", err)
	}
}
