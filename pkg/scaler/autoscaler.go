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

package scaler

import (
    "context"

    "github.com/agent-sandbox/agent-sandbox/pkg/activator"
)

type Scaler struct {
    rootCtx   context.Context
    activator *activator.Activator
}

func NewScaler(ctx context.Context) *Scaler {
    a := activator.NewActivator(ctx)
    scaler := &Scaler{
        rootCtx:   ctx,
        activator: a,
    }
    return scaler
}

func (s *Scaler) RunScaling() {

}
