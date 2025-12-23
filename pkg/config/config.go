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
    "log"

    "github.com/kelseyhightower/envconfig"
)

var Cfg *Config

type Config struct {
    APIVersion string `split_words:"true" default:"v1" required:"false"`
    APIBaseURL string `split_words:"true" default:"" required:"false"`
    ServerAddr string `split_words:"true" default:"0.0.0.0:10000" required:"false"`

    // witch Kubernetes namespace to create sandboxes Replicaset&Pod in
    SandboxNamespace string `split_words:"true" default:"default" required:"false"`

    SandboxTemplateFile string `split_words:"true" default:"" required:"false"`
}

func init() {
    var cfg Config
    if err := envconfig.Process("", &cfg); err != nil {
        log.Fatal("Failed to process config: ", err)
    }

    cfg.APIBaseURL = "/api/" + cfg.APIVersion
    Cfg = &cfg
}
