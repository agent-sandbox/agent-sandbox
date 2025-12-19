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

type Sandbox struct {
    // Set the CMD of the SandboxHandler, overriding any CMD of the container image.
    Args []string `json:"args,omitempty"`

    // Associate the sandbox with an app. Required unless creating from a container.
    App string `json:"app,omitempty"`

    // Optionally give the sandbox a name. Unique within an app.
    Name string `json:"name,omitempty" required:"true"`

    // The image to run as the container for the sandbox.
    Image string `json:"image,omitempty"`

    // The type to run as the container for the sandbox when Image is not set. e.g. aio/python/shell/
    Type string `json:"type,omitempty"`

    // Environment variables to set in the SandboxHandler.
    Env map[string]*string `json:"env,omitempty"`

    // Maximum lifetime of the sandbox in minutes. timeout is reached apply to delete action
    Timeout int `json:"timeout,omitempty" default:"300"` // default 60m

    // The amount of time in minutes that a sandbox can be idle before being terminated.
    IdleTimeout int `json:"idle_timeout,omitempty"` // default 10m

    // Policy to apply when the idle is reached. Options are 'delete' or 'scaledown'.
    IdlePolicy string `json:"idle_policy,omitempty" default:"delete"` // default delete

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

    // HTTP/2 encrypted ports
    Ports []int `json:"ports,omitempty"`

    // Status of the sandbox. Options are 'creating', 'running', 'idle', 'deleting', 'error'.
    Status string `json:"status,omitempty"`
}

var DefaultSandbox = &Sandbox{
    CPU:         "100m",
    Memory:      "128Mi",
    CPULimit:    "1000m",
    MemoryLimit: "1024Mi",
    Timeout:     60,
    IdleTimeout: 10,
    //Image:       "nginx:latest",
}

func (o *Sandbox) Make() {
    // one day max
    if o.Timeout >= 1440 {
        o.Timeout = 1440
    }
    // one hour max
    if o.IdleTimeout > 60 {
        o.IdleTimeout = 60
    }
    if o.Image == "" {
        switch o.Type {
        case "python":
            o.Image = "python:3.9-slim"
        case "shell":
            o.Image = "alpine:latest"
        case "node":
            o.Image = "node:16-alpine"
        case "aio":
            o.Image = "ghcr.io/agent-infra/sandbox:latest"
        case "aiocn":
            o.Image = "enterprise-public-cn-beijing.cr.volces.com/vefaas-public/all-in-one-sandbox:latest"
        default:
            o.Image = "nginx:latest"
        }
    }
}

type SandboxKube struct {
    Sandbox   *Sandbox
    RawData   string
    Namespace string
}

const SandboxTemplate = `apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: {{.Sandbox.Name}}
  namespace: {{.Namespace}}
  annotations:
    sandbox-data:  |
        {{.RawData}}
  labels:
    sandbox: "{{.Sandbox.Name}}"
    owner: agent-sandbox
spec:
  replicas: 1
  selector:
    matchLabels:
      sandbox: "{{.Sandbox.Name}}"
  template:
    metadata:
      labels:
        sandbox: "{{.Sandbox.Name}}"
        owner: agent-sandbox
    spec:
      containers:
      - name: sandbox
        image: {{.Sandbox.Image}}
        imagePullPolicy: IfNotPresent
        env:
        - name: INSTANCE_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        resources:
          requests:
            cpu: {{.Sandbox.CPU}}
            memory: {{.Sandbox.Memory}}
          limits:
            cpu: {{.Sandbox.CPULimit}}
            memory: {{.Sandbox.MemoryLimit}}
`
