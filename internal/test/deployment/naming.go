// Copyright 2023 Percona LLC
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

package deployment

func (pmm *PMM) TestPrefix() string {
	return "pmm-dump-test-" + pmm.testName
}

func (pmm *PMM) NetworkName() string {
	return pmm.TestPrefix() + "-network"
}

func (pmm *PMM) ServerContainerName() string {
	return pmm.TestPrefix() + "-pmm-server"
}

func (pmm *PMM) ClientContainerName() string {
	return pmm.TestPrefix() + "-pmm-client"
}

func (pmm *PMM) MongoContainerName() string {
	return pmm.TestPrefix() + "-mongo"
}

func (pmm *PMM) ServerImage() string {
	return ImageNameWithTag(pmm.pmmServerImage, pmm.pmmVersion)
}

func (pmm *PMM) ClientImage() string {
	return ImageNameWithTag(pmm.pmmClientImage, pmm.pmmVersion)
}

func (pmm *PMM) MongoImage() string {
	return ImageNameWithTag(pmm.mongoImage, pmm.mongoTag)
}

func ImageNameWithTag(image, tag string) string {
	if image == "" {
		return ""
	}
	if tag == "" {
		return "latest"
	}
	return image + ":" + tag
}
