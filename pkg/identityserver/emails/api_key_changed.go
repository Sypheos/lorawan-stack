// Copyright © 2019 The Things Network Foundation, The Things Industries B.V.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package emails

import "go.thethings.network/lorawan-stack/pkg/ttnpb"

// APIKeyChanged is the email that is sent when users updates an API key
type APIKeyChanged struct {
	Data
	Key *ttnpb.APIKey
}

// TemplateName returns the name of the template to use for this email.
func (APIKeyChanged) TemplateName() string { return "api_key_changed" }

const apiKeyChangedSubject = `An API key for {{.Network.Name}} has been created`

const apiKeyChangedText = `Dear {{.User.Name}},

The API key "{{.Key.PrettyName}}" for "{{.Entity.ID}}" on {{.Network.Name}} has been updated with the following rights:
{{range $right := .Key.Rights}} 
{{$right.PrettyName}} {{end}}
`

// DefaultTemplates returns the default templates for this email.
func (APIKeyChanged) DefaultTemplates() (subject, html, text string) {
	return apiKeyChangedSubject, "", apiKeyChangedText
}
