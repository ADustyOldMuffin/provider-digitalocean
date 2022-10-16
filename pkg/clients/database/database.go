/*
Copyright 2021 The Crossplane Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package database

import (
	"github.com/digitalocean/godo"

	"github.com/crossplane-contrib/provider-digitalocean/apis/database/v1alpha1"
	do "github.com/crossplane-contrib/provider-digitalocean/pkg/clients"
)

// GenerateDatabase generates *godo.DatabaseRequest instance from LBParameters.
func GenerateDatabase(name string, in v1alpha1.DODatabaseClusterParameters, create *godo.DatabaseCreateRequest) {
	create.Name = name
	create.EngineSlug = do.StringValue(in.Engine)
	create.Version = do.StringValue(in.Version)
	create.NumNodes = in.NumNodes
	create.SizeSlug = in.Size
	create.Region = in.Region
	create.PrivateNetworkUUID = do.StringValue(in.PrivateNetworkUUID)
	create.Tags = in.Tags
}

// LateInitializeSpec updates any unset (i.e. nil) optional fields of the
// supplied LBParameters that are set (i.e. non-zero) on the supplied
// LB.
func LateInitializeSpec(p *v1alpha1.DODatabaseClusterParameters, observed godo.Database) {
	p.Version = do.LateInitializeString(p.Version, observed.EngineSlug)
	p.PrivateNetworkUUID = do.LateInitializeString(p.PrivateNetworkUUID, observed.PrivateNetworkUUID)

	if len(p.Tags) == 0 && len(observed.Tags) != 0 {
		p.Tags = make([]string, len(observed.Tags))
		copy(p.Tags, observed.Tags)
	}
}
