package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/skill"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// NotionPackChoice is one pack as the pack picker presents it.
type NotionPackChoice struct {
	ID          string
	Name        string
	Description string
}

// NotionPackPrompter is the optional capability of a Prompter to ask which Notion packs to install.
// A prompter without it, and a run with -y, --list or --json, installs every pack.
type NotionPackPrompter interface {
	SelectNotionPacks(workspace string, packs []NotionPackChoice) ([]string, error)
}

// notionExport is one pack staged by the bare `notion` source, and the lock source of its skills.
type notionExport struct {
	source source.Source
	dir    string // slash-separated staging directory of the pack
	name   string // pack name, used as the skill group
}

// prepareNotionPacks exports the packs the bare `notion` argument selects into dir and returns the
// run source plus one entry per pack, so every installed skill is locked to its own pack.
func prepareNotionPacks(ctx context.Context, opts AddOptions, prompter Prompter,
	dir string) (source.Source, []notionExport, error) {
	if err := source.RequireNTN(); err != nil {
		return source.Source{}, nil, err
	}
	packs, err := source.FetchNotionPacks(ctx)
	if err != nil {
		return source.Source{}, nil, err
	}
	if len(packs) == 0 {
		return source.Source{}, nil, useErrorf("Notion returned no packs for the authenticated workspace")
	}
	chosen, err := chooseNotionPacks(ctx, opts, prompter, packs)
	if err != nil {
		return source.Source{}, nil, err
	}
	seen := map[string]string{}
	exports := make([]notionExport, 0, len(chosen))
	for _, pack := range chosen {
		packDir := source.NotionPackDir(pack.ID)
		if other, ok := seen[packDir]; ok {
			return source.Source{}, nil, fmt.Errorf(
				"notion packs %q and %q share the staging directory %s", other, pack.Name, packDir)
		}
		seen[packDir] = pack.Name
		if err := source.ExportNotionPack(ctx, pack, dir); err != nil {
			return source.Source{}, nil, err
		}
		lockURL := source.NotionPackURL(pack.ID)
		exports = append(exports, notionExport{
			source: source.Source{
				Kind: source.KindDownload, Source: "notion", SourceURL: lockURL, NotionPackID: pack.ID,
			},
			dir:  packDir,
			name: pack.Name,
		})
	}
	return source.Source{Kind: source.KindDownload, Source: "notion"}, exports, nil
}

// chooseNotionPacks asks which packs to install. Every pack is selected when the run may not prompt
// or the prompter cannot present the pack picker.
func chooseNotionPacks(ctx context.Context, opts AddOptions, prompter Prompter,
	packs []source.NotionPack) ([]source.NotionPack, error) {
	picker, ok := prompter.(NotionPackPrompter)
	if !promptsWanted(opts) || prompter == nil || !ok {
		return packs, nil
	}
	choices := make([]NotionPackChoice, 0, len(packs))
	for _, pack := range packs {
		choices = append(choices, NotionPackChoice{
			ID: pack.ID, Name: pack.Name, Description: pack.Description,
		})
	}
	ids, err := picker.SelectNotionPacks(source.NotionWorkspaceName(ctx), choices)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	selected := make([]source.NotionPack, 0, len(ids))
	for _, pack := range packs {
		if want[pack.ID] {
			selected = append(selected, pack)
		}
	}
	if len(selected) == 0 {
		return nil, errors.New("no Notion packs selected")
	}
	return selected, nil
}

// groupNotionPacks names the pack of every discovered skill, for grouping, and rejects a skill that
// sits outside every exported pack. Every skill of a pack run must be lockable to its own pack.
func groupNotionPacks(found *skill.Result, exports []notionExport) error {
	for i := range found.Skills {
		export, ok := notionExportOf(exports, found.Skills[i].RelDir)
		if !ok {
			return fmt.Errorf("skill %q is outside every exported Notion pack", found.Skills[i].Name)
		}
		found.Skills[i].PluginName = export.name
	}
	return nil
}

// notionExportOf finds the exported pack holding a skill by its staging directory.
func notionExportOf(exports []notionExport, relDir string) (notionExport, bool) {
	for _, export := range exports {
		if relDir == export.dir || strings.HasPrefix(relDir, export.dir+"/") {
			return export, true
		}
	}
	return notionExport{}, false
}
