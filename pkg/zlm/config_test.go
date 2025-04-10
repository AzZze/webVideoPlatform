package zlm

import (
	"fmt"
	"testing"
)

func TestEngine_GetServerConfig(t *testing.T) {
	const url = "http://127.0.0.1:8080"
	e := NewEngine().SetConfig(Config{URL: url, Secret: "s1kPE7bzqKeHUaVcp8dCA0jeB8yxyFq4"})
	out, err := e.GetServerConfig()
	if err != nil {
		t.Errorf("Engine.GetServerConfig() error = %v", err)
		return
	}
	fmt.Printf("%+v", out)
}
