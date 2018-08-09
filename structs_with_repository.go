package main

// HookWithRepository represents an event message sent by Github that contains a "repository" field.
type HookWithRepository struct {
	Ref        string `json:"ref"`
	Repository struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Url      string `json:"url"`
		Owner    struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"owner"`
		Private     bool   `json:"private"`
		HTMLURL     string `json:"html_url"`
		Description string `json:"description"`
	} `json:"repository"`
}
