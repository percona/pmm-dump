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
