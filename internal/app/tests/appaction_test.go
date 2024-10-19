package app_test

import (
	"net/http/httptest"
	"path"
	"testing"

	"github.com/claceio/clace/internal/app"
	"github.com/claceio/clace/internal/testutil"
)

func actionTester(t *testing.T, rootPath bool, actionPath string) {
	t.Helper()
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
def handler(dry_run, args):
	return ace.result(status="done", values=["a", "b"])

app = ace.app("testApp",
	actions=[ace.action("testAction", "` + actionPath + `", handler)])

		`,
		"params.star": `param("param1", description="param1 description", type=STRING, default="myvalue")`,
	}
	var a *app.App
	var err error
	if rootPath {
		a, _, err = CreateTestAppRoot(logger, fileData)
	} else {
		a, _, err = CreateTestApp(logger, fileData)
	}
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	var reqPath string
	if rootPath {
		reqPath = actionPath
	} else {
		reqPath = path.Join("/test", actionPath)
	}

	request := httptest.NewRequest("GET", reqPath, nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "<title>testAction</title>")
	testutil.AssertStringContains(t, response.Body.String(), `id="param_param1"`)

	request = httptest.NewRequest("POST", reqPath, nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "match response", response.Body.String(), `
          <div class="text-lg text-bold">
            done
          </div>
        
          <div id="action_result" hx-swap-oob="true" hx-swap="outerHTML">
            <div class="divider text-lg text-secondary">Output</div>
            <textarea
              rows="20"
              class="textarea textarea-success w-full font-mono"
              readonly>a
        b
        </textarea
            >
          </div>
        `)

}

func TestRootAppRootAction(t *testing.T) {
	actionTester(t, true, "/")
}

func TestRootApp(t *testing.T) {
	actionTester(t, true, "/abc")
}

func TestNonRootAppRootAction(t *testing.T) {
	actionTester(t, false, "/")
}

func TestNonRootApp(t *testing.T) {
	actionTester(t, false, "/abc")
}

func TestParamErrors(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
def handler(dry_run, args):
	return ace.result(status="done", values=["a", "b"], param_errors={"param1": "param1error", "param3": "param3error"})

app = ace.app("testApp",
	actions=[ace.action("testAction", "/", handler)])

		`,
		"params.star": `param("param1", description="param1 description", type=STRING, default="myvalue")
param("param2", description="param2 description", type=BOOLEAN, default=True)
param("param3", description="param3 description", type=INT, default=10)`,
	}
	a, _, err := CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "<title>testAction</title>")
	testutil.AssertStringContains(t, response.Body.String(), `id="param_param1"`)

	request = httptest.NewRequest("POST", "/test", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "match response", `
	<div class="text-lg text-bold">
            done
          </div>
        
          <div
            id="param_param1_error"
            hx-swap-oob="true"
            hx-swap="outerHTML"
            class="text-error mt-1">
            param1error
          </div>
        
          <div
            id="param_param3_error"
            hx-swap-oob="true"
            hx-swap="outerHTML"
            class="text-error mt-1">
            param3error
          </div>
        
          <div id="action_result" hx-swap-oob="true" hx-swap="outerHTML">
            <div class="divider text-lg text-secondary">Output</div>
            <textarea
              rows="20"
              class="textarea textarea-success w-full font-mono"
              readonly>a
        b
        </textarea >
          </div>
        `, response.Body.String())
}

func TestAutoReportTable(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
def handler(dry_run, args):
	return ace.result(status="done", values=[{"a": 1, "b": "abc"}])

app = ace.app("testApp",
	actions=[ace.action("testAction", "/", handler, report=ace.AUTO)])

		`,
		"params.star": `param("param1", description="param1 description", type=STRING, default="myvalue")`,
	}
	a, _, err := CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "<title>testAction</title>")
	testutil.AssertStringContains(t, response.Body.String(), `id="param_param1"`)

	request = httptest.NewRequest("POST", "/test", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "match response", `
	<div class="text-lg text-bold"> done </div>
        
            <div id="action_result" hx-swap-oob="true" hx-swap="outerHTML">
            <div class="divider text-lg text-secondary">Report</div>
        
            <table class="table table-zebra text-xl font-mono">
              <thead>
                <tr>
                    <th>a</th>
                    <th>b</th>
                </tr>
              </thead>
              <tbody>
                  <tr>
                      <td>1</td>
                      <td>abc</td>
                  </tr>
              </tbody>
            </table>
          </div>`, response.Body.String())
}

func TestAutoReportJSON(t *testing.T) {
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
def handler(dry_run, args):
	return ace.result(status="done", values=[{"a": {"c": 1}, "b": "abc"}])

app = ace.app("testApp",
	actions=[ace.action("testAction", "/", handler, report=ace.AUTO)])

		`,
		"params.star": `param("param1", description="param1 description", type=STRING, default="myvalue")`,
	}
	a, _, err := CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "<title>testAction</title>")
	testutil.AssertStringContains(t, response.Body.String(), `id="param_param1"`)

	request = httptest.NewRequest("POST", "/test", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "match response", `
		  <div class="text-lg text-bold"> done </div>
        
          <div id="action_result" hx-swap-oob="true" hx-swap="outerHTML">
            <div class="divider text-lg text-secondary">Result</div>
            <div class="json-container" data-json="{&#34;a&#34;:{&#34;c&#34;:1},&#34;b&#34;:&#34;abc&#34;}"></div>
            <script>
              document.querySelectorAll(".json-container").forEach(function (div) {
                const jsonData = JSON.parse(div.getAttribute("data-json"));
                renderJSONWithRoot(jsonData, div);
              });
            </script>
          </div>`, response.Body.String())
}

func TestReportTable(t *testing.T) {
	// Force table format for output containing map
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
def handler(dry_run, args):
	return ace.result(status="done", values=[{"a": {"c": 1}, "b": "abc"}])

app = ace.app("testApp",
	actions=[ace.action("testAction", "/", handler, report=ace.TABLE)])

		`,
		"params.star": `param("param1", description="param1 description", type=STRING, default="myvalue")`,
	}
	a, _, err := CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "<title>testAction</title>")
	testutil.AssertStringContains(t, response.Body.String(), `id="param_param1"`)

	request = httptest.NewRequest("POST", "/test", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "match response", `
	<div class="text-lg text-bold">
            done
          </div>
        
          <div id="action_result" hx-swap-oob="true" hx-swap="outerHTML">
            <div class="divider text-lg text-secondary">Report</div>
        
            <table class="table table-zebra text-xl font-mono">
              <thead>
                <tr>
                    <th>a</th>
                    <th>b</th>
                </tr>
              </thead>
              <tbody>
                  <tr>
                      <td>map[c:1]</td>
                      <td>abc</td>
                  </tr>
              </tbody>
            </table>
          </div>`, response.Body.String())
}

func TestReportTableMissingData(t *testing.T) {
	// Force table format for output containing map
	logger := testutil.TestLogger()
	fileData := map[string]string{
		"app.star": `
def handler(dry_run, args):
	return ace.result(status="done", values=[{"a": 1, "b": "abc"}, {"c": 1, "b": "abc2"}])

app = ace.app("testApp",
	actions=[ace.action("testAction", "/", handler, report=ace.TABLE)])

		`,
		"params.star": `param("param1", description="param1 description", type=STRING, default="myvalue")`,
	}
	a, _, err := CreateTestApp(logger, fileData)
	if err != nil {
		t.Fatalf("Error %s", err)
	}

	request := httptest.NewRequest("GET", "/test/", nil)
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)

	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringContains(t, response.Body.String(), "<title>testAction</title>")
	testutil.AssertStringContains(t, response.Body.String(), `id="param_param1"`)

	request = httptest.NewRequest("POST", "/test", nil)
	response = httptest.NewRecorder()
	a.ServeHTTP(response, request)
	testutil.AssertEqualsInt(t, "code", 200, response.Code)
	testutil.AssertStringMatch(t, "match response", `<div class="text-lg text-bold">
            done
          </div>
        
          <div id="action_result" hx-swap-oob="true" hx-swap="outerHTML">
            <div class="divider text-lg text-secondary">Report</div>
        
            <table class="table table-zebra text-xl font-mono">
              <thead>
                <tr>
                    <th>a</th>
                    <th>b</th>
                </tr>
              </thead>
              <tbody>
                  <tr>
                      <td>1</td>
                      <td>abc</td>
                  </tr>
                  <tr>
                      <td></td>
                      <td>abc2</td>
                  </tr>
              </tbody>
            </table>
          </div>`, response.Body.String())
}
