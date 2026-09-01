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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/google/uuid"
	v1 "k8s.io/api/apps/v1"
)

// Defines values for SandboxState.
type SandboxState string
type SandboxOnTimeout string

const (
	Paused   SandboxState = "paused"
	Running  SandboxState = "running"
	Creating SandboxState = "creating"
)

const (
	DefaultCPU         = "200m"
	DefaultMemory      = "500Mi"
	DefaultCPULimit    = "1000m"
	DefaultMemoryLimit = "2000Mi"
)

const (
	AnnotationSandboxData     = "sandbox-data"
	AnnotationPaused          = "agent-sandbox.github.io/paused"
	AnnotationPausedAt        = "agent-sandbox.github.io/paused-at"
	AnnotationPauseReason     = "agent-sandbox.github.io/pause-reason"
	AnnotationResumedAt       = "agent-sandbox.github.io/resumed-at"
	AnnotationResumeReason    = "agent-sandbox.github.io/resume-reason"
	AnnotationProcessSnapshot = "agent-sandbox.github.io/process-snapshot"
)

type SandboxBase struct {

	// Optionally give the sandbox a name.
	Name string `json:"name,omitempty" required:"false" jsonschema:"The unique name of Sandbox."`

	// The type to run as the container for the sandbox when Image is not set. e.g. aio/python/shell/
	Template string `json:"template,omitempty" required:"false" jsonschema:"The sandbox Template name."`
}

type Sandbox struct {
	SandboxBase

	// For K8s object metadata access
	//metav1.Object `json:"-"`
	ReplicaSet *v1.ReplicaSet `json:"-"`

	User string `json:"user,omitempty"`

	IsPool bool `json:"isPool,omitempty"`

	TemplateObj *config.Template `json:"-"`

	// Set the CMD of the SandboxHandler, overriding any CMD of the container image.
	Cmd string `json:"cmd,omitempty"`

	Args []string `json:"args,omitempty"`

	// ------------
	// for input params, used for create sandbox
	// ------------

	// Optionally give the sandbox a unique id.  compatible with E2B API
	ID string `json:"id,omitempty" required:"false" jsonschema:"The unique id of Sandbox."`

	// Associate the sandbox with an app. Required unless creating from a container.
	App string `json:"app,omitempty" jsonschema:"App to for associate the sandbox with an app"`

	// The image to run as the container for the sandbox.
	Image string `json:"image,omitempty"`

	// Environment variables to set in the SandboxHandler.
	EnvVars map[string]string `json:"envVars,omitempty"`

	// Maximum lifetime of the sandbox in seconds.
	Timeout int `json:"timeout,omitempty" default:"1800"` // default 30m, -1 is no timeout

	// The amount of time in seconds that a sandbox can be idle before being terminated.
	IdleTimeout int `json:"idle_timeout,omitempty"` // default 10m, -1 is no timeout

	// AutoPause Automatically pauses the sandbox after the timeout
	AutoPause bool `json:"autoPause,omitempty"`

	// AutoResume Auto-resume configuration for paused sandboxes.
	AutoResume bool `json:"autoResume,omitempty"`

	// Working directory of the sandbox.
	Workdir string `json:"workdir,omitempty"`

	// CPU request
	CPU string `json:"cpu,omitempty"  default:"100m"`

	// Memory request
	Memory string `json:"memory,omitempty"  default:"128Mi"`

	// CPU limit
	CPULimit string `json:"cpu_limit,omitempty"  default:"1000m"`

	// Memory limit
	MemoryLimit string `json:"memory_limit,omitempty"  default:"1024Mi"`

	// Port for startup probe and main service
	Port int `json:"port,omitempty"  default:"8080"`

	// Status of the sandbox. Options are 'creating', 'running', 'idle', 'deleting', 'error'.
	Status SandboxState `json:"status,omitempty"`

	// CreatedAt Time when the sandbox was started
	CreatedAt time.Time `json:"created_at,omitempty"`

	Metadata map[string]string `json:"metadata,omitempty"`
}

const (
	IDLabel   = "sbx-id"
	UserLabel = "sbx-user"
	TPLLabel  = "sbx-template"
	PoolLabel = "sbx-pool" // "true" indicates this is a pool replicaset
)

// Sandbox values process: GetDefaultSandbox->request overwrite->Make->setDefaultValueOfSandbox

func GetDefaultSandbox() *Sandbox {
	sb := &Sandbox{
		Timeout:     30 * 60, // 30 minutes
		IdleTimeout: -1,      // no idle timeout
		Port:        8080,
	}
	sb.ReplicaSet = &v1.ReplicaSet{}
	sb.ReplicaSet.SetAnnotations(map[string]string{})
	sb.ReplicaSet.SetLabels(map[string]string{})
	sb.Metadata = make(map[string]string)

	return sb
}

func validAndRestValueOfSandbox(sb *Sandbox) error {
	// one day max
	maxTimeout := 60 * 60 * 24
	if sb.Timeout > maxTimeout {
		return fmt.Errorf("invalid timeout %d, must be <= 60*60*24 (seconds), -1 is no timeout", sb.Timeout)
	}

	// default timeout, since int default is 0 when not set, so we set it to 30 minutes, E2B default is 300s
	if sb.Timeout == 0 {
		sb.Timeout = 30 * 60 // default 30 minutes
	}

	if sb.Timeout < 0 {
		sb.Timeout = -1 // no timeout
	}

	// 50m max
	// since k8s events default retention time is 1h, so the max idle timeout is set to 50m to make sure the last request event can be recorded in k8s events and can be used for idle timeout check in scaler
	maxIdleTimeout := 50 * 60
	if sb.IdleTimeout > maxIdleTimeout {
		return fmt.Errorf("invalid idle timeout %v, must <= 3000 (seconds), -1 is no idle timeout", sb.IdleTimeout)
	}

	if sb.IdleTimeout <= 0 {
		sb.IdleTimeout = -1 // no idle timeout
	}

	// check resources is not set and set to default value
	if sb.CPU == "" {
		sb.CPU = DefaultCPU
	}
	if sb.Memory == "" {
		sb.Memory = DefaultMemory
	}
	if sb.CPULimit == "" {
		sb.CPULimit = DefaultCPULimit
	}
	if sb.MemoryLimit == "" {
		sb.MemoryLimit = DefaultMemoryLimit
	}

	return nil
}

func (sb *Sandbox) ToString() string {
	sbTmp := *sb
	sbTmp.User = MaskUserKey(sbTmp.User)
	sbStr, _ := json.Marshal(sbTmp)
	return string(sbStr)
}

func (sb *Sandbox) Make() error {
	// remove '-'
	id := strings.Replace(uuid.NewString(), "-", "", -1)
	sb.ID = id

	sb.CreatedAt = time.Now()
	sb.Status = Creating

	t := &config.Template{}

	// no set any params, use default template or image
	if sb.Template == "" && sb.Image == "" {
		defTpl := config.Cfg.GetSandboxDefaultTemplate()
		defImg := config.Cfg.GetSandboxDefaultImage()

		if defTpl != "" {
			sb.Template = defTpl
		}

		if defImg != "" && defTpl == "" {
			sb.Image = defImg
		}
	}

	if sb.Template != "" {
		tpl, err := config.GetTemplateByName(sb.Template)
		if err != nil {
			return fmt.Errorf("failed to get Template by name %s: %v", sb.Template, err)
		}
		t = tpl
		// TODO request overwrite template's image with sb.Image, currently if template is set, sb.Image will be ignored, we can support overwrite in the future
		sb.Image = tpl.Image
		if tpl.Port != 0 {
			sb.Port = tpl.Port
		}
	}

	// use image create sandbox, template name not set, use "custom" as template name
	if sb.Template == "" && sb.Image != "" {
		sb.Template = "custom"
		t = &config.Template{
			Name:  sb.Template,
			Image: sb.Image,
		}
	}

	sb.TemplateObj = t

	// apply template args if sandbox has none
	if len(sb.Args) == 0 && len(t.Args) > 0 {
		sb.Args = t.Args
	}

	// merge template metadata and sandbox metadata, sandbox metadata has higher priority
	if sb.Metadata == nil {
		sb.Metadata = make(map[string]string)
	}
	if t.Metadata != nil {
		for k, v := range t.Metadata {
			if _, ok := sb.Metadata[k]; !ok {
				sb.Metadata[k] = v
			}
		}
	}

	// use template's resource if not set in sandbox
	if t.Resources.CPU != "" && sb.CPU == "" {
		sb.CPU = t.Resources.CPU
	}
	if t.Resources.Memory != "" && sb.Memory == "" {
		sb.Memory = t.Resources.Memory
	}
	if t.Resources.CPULimit != "" && sb.CPULimit == "" {
		sb.CPULimit = t.Resources.CPULimit
	}
	if t.Resources.MemoryLimit != "" && sb.MemoryLimit == "" {
		sb.MemoryLimit = t.Resources.MemoryLimit
	}

	if sb.Name == "" {
		prefix := time.Now().UnixMilli() //1784267297000

		userSig := "default"
		if sb.User != "" {
			// remove e2b_ prefix, e.g. e2b_testuser-aef134ef-7aa1-945e-9399-7df9a4ad0c3f
			userSig = strings.TrimPrefix(sb.User, "e2b_")
			l := len(userSig)
			if l > 10 {
				l = 10
			}
			userSig = userSig[:l]
		}

		// k8s name max length is 63
		postfix := id[:20]
		sb.Name = fmt.Sprintf("sbx-%s-%d-%s", userSig, prefix, postfix)
		if len(sb.Name) > 60 {
			sb.Name = sb.Name[:60]
		}
	}

	// set sandbox id to envVars
	if sb.EnvVars == nil {
		sb.EnvVars = make(map[string]string)
	}
	sb.EnvVars["AGENT_SANDBOX_ID"] = sb.ID
	sb.EnvVars["AGENT_SANDBOX_NAME"] = sb.Name

	// other default values
	if err := validAndRestValueOfSandbox(sb); err != nil {
		return err
	}

	return nil
}

type SandboxKube struct {
	Sandbox   *Sandbox
	RawData   string
	Namespace string
}

// MaskUserKey keeps the leading 2/3 of a user key (matches pkg/telemetry's
// maskUser). Public endpoints use this so anonymous callers can't read the
// full identity even when per-user counters are exposed.
func MaskUserKey(s string) string {
	n := len(s) * 2 / 3
	if n < 1 {
		return s
	}
	return s[:n]
}
