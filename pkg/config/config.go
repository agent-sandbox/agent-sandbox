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

package config

import (
    "encoding/json"
    "os"

    "github.com/kelseyhightower/envconfig"
    "k8s.io/klog/v2"
)

type Environment struct {
    Name        string `json:"name" required:"false"`
    Image       string `json:"image" required:"false"`
    Description string `json:"description" required:"false"`
}

var Cfg *Config
var Environments *[]*Environment

type Config struct {
    APIVersion string `split_words:"true" default:"v1" required:"false"`
    APIBaseURL string `split_words:"true" default:"" required:"false"`
    ServerAddr string `split_words:"true" default:"0.0.0.0:10000" required:"false"`

    // witch Kubernetes namespace to create sandboxes Replicaset&Pod in
    SandboxNamespace string `split_words:"true" default:"default" required:"false"`

    SandboxTemplateFile string `split_words:"true" default:"" required:"false"`

    SandboxEnvironmentConfigFile string `split_words:"true" default:"default-environments.json" required:"false"`
    SandboxDefaultImage          string `split_words:"true" default:"ghcr.io/agent-infra/sandbox:latest" required:"false"`
    SandboxDefaultEnvironment    string `split_words:"true" default:"aio" required:"false"`
}

func init() {
    var cfg Config
    if err := envconfig.Process("", &cfg); err != nil {
        klog.Fatal("Failed to process config: ", err)
    }

    cfg.APIBaseURL = "/api/" + cfg.APIVersion
    Cfg = &cfg

    LoadEnvironments()
}

func LoadEnvironments() {
    //load environments config, read file from cfg.SandboxEnvironmentConfigFile by os.ReadFile
    envFile := Cfg.SandboxEnvironmentConfigFile
    klog.Infof("Loading environment config from file %s", envFile)

    environments, err := os.ReadFile(envFile)
    if err != nil {
        klog.Fatalf("Failed to read environment config file %s error: %v", envFile, err)
    }

    var envs []*Environment
    err = json.Unmarshal(environments, &envs)
    if err != nil {
        klog.Fatalf("Failed to unmarshal environment config file %s error: %v", envFile, err)
    }

    //check envs not empty
    if len(envs) == 0 {
        klog.Fatalf("No environments found in config file %s", envFile)
    }

    //varify each env has name  image and description
    for _, env := range envs {
        if env.Name == "" || env.Image == "" || env.Description == "" {
            klog.Fatalf("Invalid environment config in file %s: %+v, name image and desc must not dempty", envFile, env)
        }
    }

    Environments = &envs
}

func GetEnvironmentByName(name string) *Environment {
    defaultEnvironment := &Environment{
        Name:  Cfg.SandboxDefaultEnvironment,
        Image: Cfg.SandboxDefaultImage,
    }
    for _, env := range *Environments {
        if env.Name == name {
            return env
        }
    }
    klog.Fatalf("Environment %s not found, use default Environment %v", name, defaultEnvironment)
    return defaultEnvironment
}

// GetEnvironmentsForMCPTools return json string, but exclude image field
func GetEnvironmentsForMCPTools() string {
    type EnvForTool struct {
        Name        string `json:"name"`
        Description string `json:"description"`
    }

    var envs []EnvForTool
    for _, env := range *Environments {
        envs = append(envs, EnvForTool{
            Name:        env.Name,
            Description: env.Description,
        })
    }

    envsJson, err := json.MarshalIndent(envs, "", "  ")
    if err != nil {
        klog.Errorf("Failed to marshal environments for MCP tools: %v", err)
        return ""
    }

    return string(envsJson)
}
