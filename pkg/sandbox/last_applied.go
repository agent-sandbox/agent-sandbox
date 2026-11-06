/*
 * Copyright 2025 The https://github.com/agent-sandbox/agent-sandbox Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package sandbox

import (
	"bytes"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/apps/v1"
)

// applyConfigUpdate applies a user update of the sandbox configuration to rs:
//
//	last-applied-configuration <- current sandbox-data (the previous configuration)
//	sandbox-data               <- current sandbox-data with update applied (the latest configuration)
//
// update edits the configuration as a generic JSON object, so fields it does not know are kept.
// An update that changes nothing leaves both annotations alone, so the previous configuration is not lost,
// and reports changed=false.
// Only user updates go through here; create and system changes (pause, resume, process snapshot,
// pool, scalers) never touch last-applied-configuration.
func applyConfigUpdate(rs *v1.ReplicaSet, update func(cfg map[string]any)) (changed bool, err error) {
	current := rs.Annotations[AnnotationSandboxData]
	cfg := map[string]any{}
	if current != "" {
		dec := json.NewDecoder(bytes.NewReader([]byte(current)))
		dec.UseNumber() // keep numbers exactly as stored
		if err := dec.Decode(&cfg); err != nil || cfg == nil {
			return false, fmt.Errorf("invalid %s annotation of %s: %v", AnnotationSandboxData, rs.Name, err)
		}
	}
	update(cfg)

	latest, err := json.Marshal(cfg)
	if err != nil {
		return false, err
	}
	if current != "" && jsonEqual(current, string(latest)) {
		return false, nil
	}

	anns := rs.GetAnnotations()
	if anns == nil {
		anns = map[string]string{}
	}
	if current != "" { // no sandbox-data means no previous configuration
		anns[AnnotationLastAppliedConfiguration] = current
	}
	anns[AnnotationSandboxData] = string(latest)
	rs.SetAnnotations(anns)
	return true, nil
}

// jsonEqual reports whether two JSON documents hold the same value, ignoring key order and spacing.
func jsonEqual(a, b string) bool {
	var va, vb any
	if json.Unmarshal([]byte(a), &va) != nil || json.Unmarshal([]byte(b), &vb) != nil {
		return false
	}
	ca, _ := json.Marshal(va)
	cb, _ := json.Marshal(vb)
	return bytes.Equal(ca, cb)
}

// setLabelsConfig is the configuration update of a labels change: full replace of metadata.labels,
// in the same canonical form create stores, removed when empty. Other metadata keys are kept.
func setLabelsConfig(userLabels map[string]string) func(cfg map[string]any) {
	return func(cfg map[string]any) {
		meta, _ := cfg["metadata"].(map[string]any)
		if meta == nil {
			if len(userLabels) == 0 {
				return
			}
			meta = map[string]any{}
			cfg["metadata"] = meta
		}
		if len(userLabels) == 0 {
			delete(meta, MetadataLabels)
			if len(meta) == 0 { // omitted when empty, like Sandbox.Metadata
				delete(cfg, "metadata")
			}
			return
		}
		meta[MetadataLabels] = FormatLabels(userLabels)
	}
}
