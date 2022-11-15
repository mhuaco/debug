package debug

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/TykTechnologies/tyk/apidef"
	"github.com/TykTechnologies/tyk/ctx"
	"github.com/TykTechnologies/tyk/log"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
)

var logger = *log.Get()

type TykSim struct {
	Request         *http.Request
	Response        *http.Response
	ResponseWriter  http.ResponseWriter
	MiddlewareHooks struct {
		Pre           func(rw http.ResponseWriter, req *http.Request)
		PostKeyAuth   func(rw http.ResponseWriter, req *http.Request)
		TykRequestMw  func(rw http.ResponseWriter, req *http.Request)
		Post          func(rw http.ResponseWriter, req *http.Request)
		TykResponseMw func(rw http.ResponseWriter, res *http.Response, req *http.Request)
		Response      func(rw http.ResponseWriter, res *http.Response, req *http.Request)
	}
}

// NewTykSim creates a new TykSim instance
func NewTykSim(inboundUrl string, apiFilePath string) *TykSim {
	apiDef, err := apiDefLoadFile(apiFilePath)
	if err != nil {
		panic(err)
	}

	inReq, err := http.NewRequest("GET", inboundUrl, nil)
	if err != nil {
		panic(err)
	}

	// Inject definition into request context
	ctx.SetDefinition(inReq, apiDef)

	return &TykSim{
		Request:        inReq,
		ResponseWriter: httptest.NewRecorder(),
	}
}

// Start starts the simulated request flow
func (sim *TykSim) Start() {
	origReq := sim.Request.Clone(sim.Request.Context())

	// Run the pre request hook
	if sim.MiddlewareHooks.Pre != nil {
		sim.MiddlewareHooks.Pre(sim.ResponseWriter, sim.Request)
	}

	// Run the post key auth hook
	if sim.MiddlewareHooks.PostKeyAuth != nil {
		sim.MiddlewareHooks.PostKeyAuth(sim.ResponseWriter, sim.Request)
	}

	// Run the Tyk built-in request middleware simulation
	if sim.MiddlewareHooks.TykRequestMw != nil {
		sim.MiddlewareHooks.TykRequestMw(sim.ResponseWriter, sim.Request)
	}

	// Run the post request hook
	if sim.MiddlewareHooks.Post != nil {
		sim.MiddlewareHooks.Post(sim.ResponseWriter, sim.Request)
	}

	// Set up outbound request URL
	apidef := ctx.GetDefinition(sim.Request)
	sim.Request.Method = apidef.Protocol
	newURL, _ := url.Parse(apidef.Proxy.TargetURL)
	if origReq.URL.Path != sim.Request.URL.Path {
		newURL.Path = sim.Request.URL.Path
	}
	if origReq.URL.RawQuery != sim.Request.URL.RawQuery {
		newURL.RawQuery = sim.Request.URL.RawQuery
	}
	sim.Request.URL = newURL

	// Make outbound request
	client := &http.Client{}
	res, err := client.Do(sim.Request)
	if err != nil {
		panic(err)
	}

	// Run the response hook
	if sim.MiddlewareHooks.Response != nil {
		sim.MiddlewareHooks.Response(sim.ResponseWriter, sim.Response, sim.Request)
	}

	// Dump the response body to console
	var output []byte
	defer res.Body.Close()
	var prettyJSON bytes.Buffer
	bodyBytes, _ := ioutil.ReadAll(res.Body)
	var trim int = 1000
	error := json.Indent(&prettyJSON, bodyBytes, "", "\t")
	if error != nil {
		output = bodyBytes
	} else {
		output = prettyJSON.Bytes()
	}
	if len(output) > trim {
		output = output[:trim]
		output = append(output, []byte("\n...(trimmed at " + string(trim) + " bytes)")...)
	}
	fmt.Print("---- Response Body ----\n", string(output))

}

func LogAsJSON(prefix string, v interface{}) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		logger.Info(fmt.Sprint(prefix), "Error marshalling JSON:", err)
	}
	// Trim to 300 bytes
	if len(b) > 300 {
		b = b[:300]
		// Add ellipsis
		b = append(b, []byte("\n...(trimmed at 300 bytes)")...)
	}

	if len(b) > 0 {
		logger.Info(fmt.Sprint(prefix+"\n"), string(b))
	} else {
		logger.Info(prefix, "Empty JSON")
	}
}

// apiDefLoadFile loads an API definition from a file
func apiDefLoadFile(path string) (*apidef.APIDefinition, error) {
	f, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	def := struct {
		ApiDefinition apidef.APIDefinition `json:"api_definition"`
	}{}
	if err := json.NewDecoder(bytes.NewReader(f)).Decode(&def); err != nil {
		return nil, err
	}
	return &def.ApiDefinition, nil
}
