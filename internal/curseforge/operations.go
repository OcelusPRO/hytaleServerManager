package curseforge

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"hytale-server-manager/internal/archive"
)

// ErrUnsupportedPack is returned for modpacks without a CurseForge manifest.
var ErrUnsupportedPack = errors.New("format de modpack non reconnu (manifest.json introuvable)")

const maxManifestBytes = 8 << 20

// StartUpdates updates the given mod files (all outdated ones when empty)
// in the background.
func (s *Service) StartUpdates(saveID string, fileNames []string) (Job, error) {
	if _, err := s.Mods.Links(saveID); err != nil {
		return Job{}, err
	}
	return s.startJob(saveID, "Mise à jour des mods", func(ctx context.Context, h *JobHandle) error {
		h.Progress(0, "Recherche des mises à jour…")
		updates, projects, err := s.findUpdates(ctx, saveID)
		if err != nil {
			return err
		}
		if len(fileNames) > 0 {
			updates = slices.DeleteFunc(updates, func(u UpdateInfo) bool { return !slices.Contains(fileNames, u.FileName) })
		}
		if len(updates) == 0 {
			h.Progress(1, "Tout est à jour")
			return nil
		}
		for i, u := range updates {
			h.Progress(float64(i)/float64(len(updates)), "Mise à jour de "+u.Name)
			project := projects[u.ProjectID]
			file, ok := pickFile(project.LatestFiles)
			if !ok {
				continue
			}
			if _, err := s.install(ctx, saveID, project, file, ""); err != nil {
				h.Warn(fmt.Sprintf("%s : %v", u.Name, err))
			}
		}
		return nil
	})
}

type packManifest struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Overrides string `json:"overrides"`
	Files     []struct {
		ProjectID int  `json:"projectID"`
		FileID    int  `json:"fileID"`
		Required  bool `json:"required"`
	} `json:"files"`
}

// StartModpack installs a modpack (fileID 0 = latest) in the background.
func (s *Service) StartModpack(saveID string, packID, fileID int) (Job, error) {
	if _, err := s.Mods.Links(saveID); err != nil {
		return Job{}, err
	}
	return s.startJob(saveID, "Installation d'un modpack", func(ctx context.Context, h *JobHandle) error {
		return s.installModpack(ctx, saveID, packID, fileID, h)
	})
}

func (s *Service) installModpack(ctx context.Context, saveID string, packID, fileID int, h *JobHandle) error {
	h.Progress(0, "Récupération du modpack…")
	pack, err := s.Client.Mod(ctx, packID)
	if err != nil {
		return err
	}
	file, err := s.chooseFile(ctx, pack, fileID)
	if err != nil {
		return err
	}
	h.Progress(0.02, "Téléchargement de « "+pack.Name+" »…")
	zipPath, err := s.download(ctx, pack, file)
	if err != nil {
		return err
	}
	defer os.Remove(zipPath)

	manifest, err := readManifest(zipPath)
	if err != nil {
		return err
	}
	packName := pack.Name
	if manifest.Name != "" {
		packName = manifest.Name
	}

	var fileIDs, projectIDs []int
	for _, f := range manifest.Files {
		if !f.Required {
			continue
		}
		fileIDs = append(fileIDs, f.FileID)
		if !slices.Contains(projectIDs, f.ProjectID) {
			projectIDs = append(projectIDs, f.ProjectID)
		}
	}
	h.Progress(0.05, "Résolution des fichiers du pack…")
	files := map[int]File{}
	for chunk := range slices.Chunk(fileIDs, batchSize) {
		list, err := s.Client.Files(ctx, chunk)
		if err != nil {
			return err
		}
		for _, f := range list {
			files[f.ID] = f
		}
	}
	projects := map[int]Mod{}
	for chunk := range slices.Chunk(projectIDs, batchSize) {
		list, err := s.Client.Mods(ctx, chunk)
		if err != nil {
			return err
		}
		for _, m := range list {
			projects[m.ID] = m
		}
	}

	total := len(fileIDs)
	for i, id := range fileIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, ok := files[id]
		if !ok {
			h.Warn(fmt.Sprintf("fichier %d introuvable sur CurseForge", id))
			continue
		}
		project, ok := projects[f.ModID]
		if !ok {
			project = Mod{ID: f.ModID, Name: f.DisplayName}
		}
		h.Progress(0.1+0.8*float64(i)/float64(total), fmt.Sprintf("%s (%d/%d)", project.Name, i+1, total))
		if _, err := s.install(ctx, saveID, project, f, packName); err != nil {
			h.Warn(fmt.Sprintf("%s : %v", project.Name, err))
		}
	}

	h.Progress(0.92, "Copie des fichiers de configuration…")
	return s.applyOverrides(zipPath, saveID, manifest.Overrides)
}

func readManifest(zipPath string) (packManifest, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return packManifest{}, ErrUnsupportedPack
	}
	defer r.Close()
	for _, f := range r.File {
		if !strings.EqualFold(f.Name, "manifest.json") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return packManifest{}, err
		}
		defer rc.Close()
		var m packManifest
		if err := json.NewDecoder(io.LimitReader(rc, maxManifestBytes)).Decode(&m); err != nil {
			return packManifest{}, fmt.Errorf("manifest du modpack illisible : %w", err)
		}
		if m.Overrides == "" {
			m.Overrides = "overrides"
		}
		return m, nil
	}
	return packManifest{}, ErrUnsupportedPack
}

// applyOverrides copies the pack's "overrides" folder into the save.
func (s *Service) applyOverrides(zipPath, saveID, overrides string) error {
	dest, err := s.Mods.SaveDir(saveID)
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(s.TmpDir, "modpack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := archive.ExtractZip(zipPath, staging, s.PackLimits); err != nil {
		return err
	}
	src, err := archive.SafeJoin(staging, overrides)
	if err != nil {
		return err
	}
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return nil // pack without overrides
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(filepath.Base(rel), ".hsm-") {
			return nil // manager metadata is never overwritten by a pack
		}
		target := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}
