package saves

import (
	"path/filepath"
	"time"

	"hytale-server-manager/internal/fsutil"
)

// linksFile records, inside the save directory, which mod files come from
// CurseForge. It travels with backups and exports of the save.
const linksFile = ".hsm-mods.json"

// CurseForgeLink ties an installed mod file to its CurseForge project.
type CurseForgeLink struct {
	ProjectID   int       `json:"projectId"`
	FileID      int       `json:"fileId"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug,omitempty"`
	URL         string    `json:"url,omitempty"`
	FileDate    time.Time `json:"fileDate"`
	InstalledAt time.Time `json:"installedAt"`
	// Pack names the modpack that brought the file, if any.
	Pack string `json:"pack,omitempty"`
}

// Links returns the CurseForge links of a save, keyed by file name.
func (m *ModStore) Links(saveID string) (map[string]CurseForgeLink, error) {
	dir, err := m.saves.Dir(saveID)
	if err != nil {
		return nil, err
	}
	m.linksMu.Lock()
	defer m.linksMu.Unlock()
	return readLinks(dir)
}

// Link records that fileName comes from CurseForge.
func (m *ModStore) Link(saveID, fileName string, link CurseForgeLink) error {
	return m.editLinks(saveID, func(links map[string]CurseForgeLink) { links[fileName] = link })
}

// Unlink forgets the CurseForge link of fileName.
func (m *ModStore) Unlink(saveID, fileName string) error {
	return m.editLinks(saveID, func(links map[string]CurseForgeLink) { delete(links, fileName) })
}

func (m *ModStore) editLinks(saveID string, edit func(map[string]CurseForgeLink)) error {
	dir, err := m.saves.Dir(saveID)
	if err != nil {
		return err
	}
	m.linksMu.Lock()
	defer m.linksMu.Unlock()
	links, err := readLinks(dir)
	if err != nil {
		return err
	}
	edit(links)
	return fsutil.WriteJSONAtomic(filepath.Join(dir, linksFile), links, 0o640)
}

func readLinks(saveDir string) (map[string]CurseForgeLink, error) {
	links := map[string]CurseForgeLink{}
	if _, err := fsutil.ReadJSON(filepath.Join(saveDir, linksFile), &links); err != nil {
		return nil, err
	}
	return links, nil
}

// SaveDir returns the working directory of a save.
func (m *ModStore) SaveDir(saveID string) (string, error) {
	return m.saves.Dir(saveID)
}
