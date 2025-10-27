package main

type PaloMeta struct {
	SourceCodeAnalysis struct {
		Score   int    `json:"score"`
		Review  string `json:"review"`
		Updated string `json:"updated"`
	} `json:"sourceCodeAnalysis" doc:"Source Code Analysis"`
	AuthorAnalysis struct {
		Score   int    `json:"score"`
		Review  string `json:"review"`
		Updated string `json:"updated"`
	} `json:"authorAnalysis" doc:"Source Code Analysis"`
}

type Server struct {
	Schema      string `json:"$schema"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Repository  struct {
		URL    string `json:"url,omitempty"`
		Source string `json:"source,omitempty"`
		ID     string `json:"id,omitempty"`
	} `json:"repository,omitempty"`
	Version  string `json:"version"`
	Packages []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Transport    struct {
			Type string `json:"type"`
		} `json:"transport"`
		EnvironmentVariables []struct {
			Description string `json:"description"`
			Name        string `json:"name"`
			Format      string `json:"format,omitempty"`
			IsSecret    bool   `json:"isSecret,omitempty"`
		} `json:"environmentVariables,omitempty"`
	} `json:"packages"`
}

type npmRegistryResponse struct {
	Versions map[string]struct {
		Dist struct {
			Tarball string `json:"tarball"`
		} `json:"dist"`
	} `json:"versions"`
	DistTags map[string]string `json:"dist-tags"`
}

type ReviewResult struct {
	File    string
	Score   int
	Comment string
}
