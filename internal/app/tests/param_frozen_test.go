package app_test

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/openrundev/openrun/internal/testutil"
)

// The app level param values are shared across requests, so list and dict
// params are frozen: a handler mutating one gets an error instead of
// leaking the change into later requests
func TestParamValuesFrozen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]string
		hosts  string
		creds  string
	}{
		{"defaults", nil, `["a"]`, `{"a": "b"}`},
		{"configured", map[string]string{"hosts": `["m"]`, "creds": `{"m": "n"}`}, `["a"]`, `{"a": "b"}`},
		{"template default", nil, `["{{ 1 }}"]`, `{"a": "{{ 2 }}"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := testutil.TestLogger()
			fileData := map[string]string{
				"app.star": `
def handler(req):
	op = req.Query.get("op")
	if op == ["list"]:
		param.hosts.append("x")
	elif op == ["dict"]:
		param.creds["k"] = "v"
	return {"list": len(param.hosts), "dict": len(param.creds)}

app = ace.app("testApp", routes=[ace.api("/", handler)])
`,
				"params.star": `param("hosts", type=LIST, default=` + tc.hosts + `)
param("creds", type=DICT, default=` + tc.creds + `)`,
			}
			a, _, err := CreateTestAppParams(logger, fileData, tc.params)
			if err != nil {
				t.Fatalf("Error %s", err)
			}

			for _, op := range []string{"list", "dict"} {
				request := httptest.NewRequest("GET", "/test/?op="+op, nil)
				response := httptest.NewRecorder()
				a.ServeHTTP(response, request)
				testutil.AssertEqualsInt(t, op+" code", 500, response.Code)
				if !strings.Contains(response.Body.String(), "frozen") {
					t.Errorf("%s: expected frozen error, got %s", op, response.Body.String())
				}
			}

			request := httptest.NewRequest("GET", "/test/", nil)
			response := httptest.NewRecorder()
			a.ServeHTTP(response, request)
			testutil.AssertEqualsInt(t, "code", 200, response.Code)
			assertJSONMatch(t, "unchanged", `{"list": 1, "dict": 1}`, response.Body.String())
		})
	}
}

// Concurrent handlers reading the shared param values do not race
func TestParamValuesConcurrentRead(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
def handler(req):
	n = 0
	for h in param.hosts:
		n += len(h)
	for k in param.creds:
		n += len(param.creds[k])
	return {"n": n}

app = ace.app("testApp", routes=[ace.api("/", handler)])
`,
		"params.star": `param("hosts", type=LIST, default=["ab", "cd"])
param("creds", type=DICT, default={"k": "xyz"})`,
	}
	a, _, err := CreateTestAppParams(logger, fileData, nil)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				request := httptest.NewRequest("GET", "/test/", nil)
				response := httptest.NewRecorder()
				a.ServeHTTP(response, request)
				if response.Code != 200 || !strings.Contains(response.Body.String(), `"n":7`) {
					t.Errorf("code %d body %s", response.Code, response.Body.String())
				}
			}
		}()
	}
	wg.Wait()
}
