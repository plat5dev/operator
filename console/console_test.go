package console

import (
	"strings"
	"testing"

	"github.com/plat5dev/operator/gateway"
)

func TestListsRoutes(t *testing.T) {
	routes, err := gateway.Load("../routes.yml")
	if err != nil {
		t.Fatal(err)
	}
	html := string(indexHTML)
	for _, rt := range routes {
		for _, method := range rt.Methods {
			label := method + " " + rt.Path
			if !strings.Contains(html, `"`+label+`"`) {
				t.Fatalf("console missing %s", label)
			}
		}
	}
}
