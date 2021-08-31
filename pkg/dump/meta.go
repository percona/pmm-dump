package dump

const (
	MetaFilename = "meta.json"
)

type Meta struct {
	GitBranch        string      `json:"git-branch"`
	GitCommit        string      `json:"git-commit"`
	PMMServerVersion string      `json:"pmm-server_version"`
	Sources          MetaSources `json:"sources"`
}

type MetaSources struct {
	QAN  bool `json:"qan"`
	Core bool `json:"core"`
}

func NewMeta(branch, commit, pmmVersion string, s MetaSources) Meta {
	return Meta{
		GitBranch:        branch,
		GitCommit:        commit,
		PMMServerVersion: pmmVersion,
		Sources:          s,
	}
}

func NewMetaSources(qan, core bool) MetaSources {
	return MetaSources{
		QAN:  qan,
		Core: core,
	}
}
