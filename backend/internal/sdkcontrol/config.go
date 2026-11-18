package sdkcontrol

import (
	"bytes"
	"encoding/json"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

type Config struct {
	PackageID, DataDir string
	// ExecutorDigest and PolicyDigest are this build's pins; root.run refuses
	// an executor that holds another build's.
	ExecutorDigest, PolicyDigest string
	// Portal serves portal.status; PortalExecutor adds the executor's entry
	// routes below DSM 7.2.
	Portal, PortalExecutor bool
}

// decodeParams decodes params strictly: unknown fields are an error.
func decodeParams(params json.RawMessage, into any) error {
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(params))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return api.BadRequest("invalid params: " + err.Error())
	}
	return nil
}
