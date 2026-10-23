package chatwrightplugin

import (
	"io/fs"
	"os"
	"testing"

	"github.com/strongo/cli-helpers/skillsync"
)

func TestSkillsFSEmbedsEveryPublishedSkill(t *testing.T) {
	onDisk, err := os.ReadDir("skills")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := fs.Sub(SkillsFS, "skills")
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := skillsync.Discover(embedded)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered) != len(onDisk) {
		t.Fatalf("embedded skills = %d, on disk = %d", len(discovered), len(onDisk))
	}
	for _, entry := range onDisk {
		if !entry.IsDir() {
			continue
		}
		found := false
		for _, skill := range discovered {
			if skill.Name == entry.Name() {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("published skill %q is missing from the embedded bundle", entry.Name())
		}
	}
}
