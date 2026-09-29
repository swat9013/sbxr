package doctor

import (
	"testing"

	"github.com/swat9013/sbxr/internal/secret"
)

func TestSetupCommandNamesOnlyACommandThatWritesTheKeyOfTheDefinition(t *testing.T) {
	defs := map[string]secret.Definition{
		secret.GitHubName: {Service: "github", Key: "GITHUB_TOKEN", Hosts: []string{"api.github.com"}},
		"api":             {Key: "API_TOKEN", Hosts: []string{"api.example.com"}, Env: "API_TOKEN"},
		"shared-a":        {Key: "A_TOKEN", Hosts: []string{"shared.example.com", "a.example.com"}, Env: "A_TOKEN"},
		"shared-b":        {Key: "B_TOKEN", Hosts: []string{"shared.example.com"}, Env: "B_TOKEN"},
		"service":         {Service: "other", Key: "OTHER_TOKEN", Hosts: []string{"other.example.com"}},
	}
	for _, tt := range []struct {
		name, want string
		found      bool
	}{
		{name: secret.GitHubName, want: "sbxr secret setup github", found: true},
		{name: "api", want: "sbxr secret setup custom --host api.example.com", found: true},
		// shared.example.com は key が 1 つに決まらないので、この定義だけが持つ host を案内する
		{name: "shared-a", want: "sbxr secret setup custom --host a.example.com", found: true},
		{name: "shared-b", found: false},
		{name: "service", found: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, found := setupCommand(tt.name, defs)

			if got != tt.want || found != tt.found {
				t.Errorf("setupCommand(%q) = %q, %v, want %q, %v", tt.name, got, found, tt.want, tt.found)
			}
		})
	}
}
