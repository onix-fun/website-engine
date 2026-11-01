package integration

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"websiteengine/features/sitebuilder"
	"websiteengine/pkg/environment"
)

func TestMetadataPublicationRendersShellAndPromotesWithSnapshot(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	var actor uuid.UUID
	if err := env.pool.QueryRow(ctx, `SELECT id FROM access_tokens LIMIT 1`).Scan(&actor); err != nil {
		t.Fatal(err)
	}

	state, err := env.snapshots.GetMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pageTitle := "Главная страница"
	metadata := sitebuilder.SiteMetadata{
		FormatVersion: 1,
		SiteName:      "VitaSil",
		Description:   "Качество продукции",
		Pages: map[string]sitebuilder.SiteMetadataOverride{
			"home": {SiteName: &pageTitle},
		},
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	state, err = env.snapshots.SaveMetadata(ctx, actor, state.Revision, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.snapshots.PublishMetadata(ctx, actor); err != nil {
		t.Fatal(err)
	}
	snapshot, err := env.snapshots.Create(ctx, actor, "SEO snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.snapshots.Deploy(ctx, actor, snapshot.ID, environment.Dev); err != nil {
		t.Fatal(err)
	}

	shell, found, err := env.snapshots.HTMLShell(ctx, environment.Dev, "/")
	if err != nil || !found {
		t.Fatalf("dev HTML shell: found=%v err=%v", found, err)
	}
	if !strings.Contains(string(shell), "<title>Главная страница</title>") || !strings.Contains(string(shell), `content="Качество продукции"`) {
		t.Fatalf("dev HTML shell has no published page metadata:\n%s", shell)
	}

	metadata.Description = "Новое описание без нового снапшота"
	encoded, err = json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	state, err = env.snapshots.GetMetadata(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state, err = env.snapshots.SaveMetadata(ctx, actor, state.Revision, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.snapshots.PublishMetadata(ctx, actor); err != nil {
		t.Fatal(err)
	}
	shell, found, err = env.snapshots.HTMLShell(ctx, environment.Dev, "/")
	if err != nil || !found || !strings.Contains(string(shell), `content="Новое описание без нового снапшота"`) {
		t.Fatalf("metadata publication did not rebuild the active shell: found=%v err=%v html=%s", found, err, shell)
	}
	snapshots, err := env.snapshots.List(ctx, 20, nil)
	if err != nil || len(snapshots) != 1 || snapshots[0].ID != snapshot.ID {
		t.Fatalf("metadata publication unexpectedly changed snapshots: %#v err=%v", snapshots, err)
	}

	composition, err := env.snapshots.Runtime(ctx, environment.Dev, "/", url.Values{})
	if err != nil || composition.Metadata.Title != pageTitle || composition.Metadata.Description != metadata.Description {
		t.Fatalf("runtime metadata does not match published metadata: %#v err=%v", composition.Metadata, err)
	}
	if _, err := env.snapshots.PromoteMetadata(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := env.snapshots.Promote(ctx, actor, snapshot.ID); err != nil {
		t.Fatal(err)
	}
	shell, found, err = env.snapshots.HTMLShell(ctx, environment.Prod, "/")
	if err != nil || !found || !strings.Contains(string(shell), `content="Новое описание без нового снапшота"`) {
		t.Fatalf("prod HTML shell was not rendered from promoted metadata: found=%v err=%v html=%s", found, err, shell)
	}
}
