// NAS acceptance fixture only. Never included in an SPK. Build with the pinned
// upstream Gopeed module (bbolt dependency), and run only while Gopeed is stopped.
// It changes metadata for the official sample only; all other DB keys remain intact.
package main

import (
	"encoding/json"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: plugin-seed <gopeed.db> <old|broken-source|restore>")
	}
	mode := os.Args[2]
	if mode != "old" && mode != "broken-source" && mode != "restore" {
		panic("unknown fixture mode")
	}
	db, e := bolt.Open(os.Args[1], 0600, &bolt.Options{Timeout: time.Second})
	if e != nil {
		panic(e)
	}
	defer db.Close()
	e = db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("extension"))
		if bucket == nil {
			return fmt.Errorf("extension bucket missing")
		}
		key := []byte("gopeed@github-release")
		var record map[string]any
		if json.Unmarshal(bucket.Get(key), &record) != nil {
			return fmt.Errorf("sample not installed")
		}
		repo, ok := record["repository"].(map[string]any)
		if !ok || repo["url"] != "https://github.com/GopeedLab/gopeed-extension-samples" || record["author"] != "gopeed" || record["name"] != "github-release" {
			return fmt.Errorf("fixture identity mismatch")
		}
		if mode == "old" {
			record["version"] = "0.9.0"
			repo["directory"] = "github-release-sample"
		}
		if mode == "broken-source" {
			repo["directory"] = "missing-plugin-fixture"
		}
		if mode == "restore" {
			repo["directory"] = "github-release-sample"
		}
		raw, e := json.Marshal(record)
		if e != nil {
			return e
		}
		return bucket.Put(key, raw)
	})
	if e != nil {
		panic(e)
	}
	fmt.Println("official sample fixture metadata updated:", mode)
}
