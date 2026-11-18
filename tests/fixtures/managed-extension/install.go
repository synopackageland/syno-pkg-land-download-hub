// Copied only into the isolated fixture package by build-owned-package.py.
// Never linked into the shipping App. It seeds one fixed inert extension via
// Gopeed's documented local-folder install API, without editing its database.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

//go:embed fixture-extension.json
var fixtureManifest []byte

func fixtureExtensionRoutes(data string) []api.Route {
	return []api.Route{api.Handle("fixture.extension.install", func(ctx context.Context, caller api.Caller, raw json.RawMessage) (any, error) {
		if err := api.RequireAdmin(caller); err != nil {
			return nil, err
		}
		if string(raw) != "{}" || os.Geteuid() == 0 {
			return nil, errors.New("invalid fixture invocation")
		}
		folder := filepath.Join(data, "owned-extension-fixture")
		if err := os.MkdirAll(folder, 0700); err != nil {
			return nil, errors.New("fixture directory unavailable")
		}
		if err := os.WriteFile(filepath.Join(folder, "manifest.json"), fixtureManifest, 0600); err != nil {
			return nil, errors.New("fixture manifest unavailable")
		}
		token, err := os.ReadFile(filepath.Join(data, "engine.token"))
		if err != nil || len(token) != 64 {
			return nil, errors.New("fixture engine unavailable")
		}
		body, _ := json.Marshal(map[string]any{"url": folder, "devMode": true})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:26943/api/v1/extensions", bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("fixture request unavailable")
		}
		req.Header.Set("X-Api-Token", string(token))
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, errors.New("fixture install unavailable")
		}
		defer response.Body.Close()
		var result struct {
			Code int    `json:"code"`
			Data string `json:"data"`
		}
		if json.NewDecoder(response.Body).Decode(&result) != nil || result.Code != 0 || result.Data != "download-hub-fixture@coverage" {
			return nil, errors.New("fixture install refused")
		}
		return map[string]string{"id": result.Data}, nil
	})}
}
