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

package activator

import (
    "context"

    "github.com/agent-sandbox/agent-sandbox/pkg/client"
    "k8s.io/client-go/tools/record"
)

func getRecorder(ctx context.Context) record.EventRecorder {
    r := client.CreateRecorderEventImpl(ctx, ComponentName)
    return r
}
