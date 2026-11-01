package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"websiteengine/features/media"
)

type integrationRenditionProcessor struct {
	failNext bool
}

func (p *integrationRenditionProcessor) Process(_ context.Context, _ media.Media, profile media.FieldProfile, _ []byte) (map[string]media.ProcessedOutput, error) {
	if p.failNext {
		p.failNext = false
		return nil, errors.New("simulated ffmpeg failure")
	}
	outputs := make(map[string]media.ProcessedOutput, len(profile.Renditions)+2)
	if profile.Accept == "video" {
		video := profile.Video
		outputs["video"] = media.ProcessedOutput{Data: []byte("h264"), MIMEType: "video/mp4", Width: video.MaxWidth, Height: video.MaxHeight}
		outputs["poster"] = media.ProcessedOutput{Data: []byte("webp-poster"), MIMEType: "image/webp", Width: video.Poster.Width, Height: video.Poster.Height}
		return outputs, nil
	}
	for _, rendition := range profile.Renditions {
		outputs[rendition.Name] = media.ProcessedOutput{
			Data: []byte("webp-" + rendition.Name), MIMEType: "image/webp",
			Width: rendition.Width, Height: rendition.Height,
		}
	}
	return outputs, nil
}

func (*integrationRenditionProcessor) Ready(context.Context) error { return nil }

func TestMediaRenditionsQueueRetryRestartPublishAndRuntime(t *testing.T) {
	env := newTestEnv(t)
	env.content.SetSchemaProvider(env.snapshots)
	env.content.SetMediaValidator(env.media)

	const featureKey = "responsive-media"
	mediaField := map[string]any{
		"type": "string", "format": "media", "title": "Обложка",
		"x-media": map[string]any{
			"accept": "raster-image",
			"renditions": []any{
				map[string]any{"name": "card", "width": 640, "height": 480, "fit": "cover"},
				map[string]any{"name": "large", "width": 1280, "height": 960, "fit": "contain"},
			},
		},
	}
	schema := objectSchema(map[string]any{"cover": mediaField}, "cover")
	manifest := manifestFor(featureKey, "1.0.0", "single", schema)
	manifest["engineApi"] = "2"
	build := uploadFeatureBuild(t, env, manifest, "export default function Feature() {}")
	saveAndPublishSnapshot(t, env, singlePageConfig(featureKey, "1.0.0"), "media profile")

	uploaded := env.uploadMedia("cover.png", minimalPNG(t))
	mediaID := uploaded["id"].(string)
	featureID := build["featureId"].(string)
	status, entry, _ := env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{"cover": mediaID}})
	if status != http.StatusCreated {
		t.Fatalf("save draft while rendition is not ready: status=%d body=%v", status, entry)
	}
	entryID := entry["id"].(string)
	revision := entry["revision"].(float64)
	env.processor.failNext = true
	status, publishBody, _ := env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/publish", env.adminToken,
		map[string]any{"revision": revision})
	if status != http.StatusConflict {
		t.Fatalf("publishing must wait for required renditions: status=%d body=%v", status, publishBody)
	}

	stateQuery := url.Values{
		"featureKey":  {featureKey},
		"schemaKind":  {"content"},
		"fieldPath":   {"/cover"},
		"buildDigest": {build["digest"].(string)},
	}
	statePath := "/api/v2/admin/media/" + mediaID + "/renditions/state?" + stateQuery.Encode()
	status, state, _ := env.request(http.MethodGet, statePath, env.adminToken, nil)
	if status != http.StatusOK || state["status"] != "pending" {
		t.Fatalf("queued rendition state: status=%d body=%v", status, state)
	}

	processed, err := env.media.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("run failed rendition job: processed=%t err=%v", processed, err)
	}
	status, state, _ = env.request(http.MethodGet, statePath, env.adminToken, nil)
	if status != http.StatusOK || state["status"] != "failed" {
		t.Fatalf("failed rendition state should be visible: status=%d body=%v", status, state)
	}

	preparePath := "/api/v2/admin/media/" + mediaID + "/renditions/prepare"
	status, retried, _ := env.request(http.MethodPost, preparePath, env.adminToken, map[string]any{
		"featureKey": featureKey, "schemaKind": "content", "fieldPath": "/cover", "buildDigest": build["digest"],
	})
	if status != http.StatusAccepted || retried["status"] != "pending" {
		t.Fatalf("retry rendition job: status=%d body=%v", status, retried)
	}

	// A new service instance consumes the pending database job, representing a
	// worker restart between queueing and processing.
	restartedWorker := media.NewServiceWithProcessor(media.NewRepository(env.pool), env.store, 20<<20, env.processor)
	processed, err = restartedWorker.ProcessOne(context.Background())
	if err != nil || !processed {
		t.Fatalf("resume rendition after worker restart: processed=%t err=%v", processed, err)
	}
	status, state, _ = env.request(http.MethodGet, statePath, env.adminToken, nil)
	if status != http.StatusOK || state["status"] != "ready" {
		t.Fatalf("completed rendition state: status=%d body=%v", status, state)
	}

	status, published, _ := env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/publish", env.adminToken,
		map[string]any{"revision": revision})
	if status != http.StatusOK {
		t.Fatalf("publish after renditions are ready: status=%d body=%v", status, published)
	}

	placement := runtimePlacement(t, env, env.devServer.URL, "/")
	cover := placement["content"].(map[string]any)["cover"].(map[string]any)
	if cover["kind"] != "image" || cover["id"] != mediaID || cover["originalUrl"] == nil || cover["srcSet"] == nil {
		t.Fatalf("runtime should return a hydrated responsive image: %v", cover)
	}
	variants := cover["variants"].(map[string]any)
	card := variants["card"].(map[string]any)
	cardURL := card["url"].(string)
	if !strings.Contains(cover["srcSet"].(string), fmt.Sprintf("%s 640w", cardURL)) {
		t.Fatalf("runtime srcSet does not reference the rendition: %v", cover)
	}

	request, _ := http.NewRequest(http.MethodGet, env.devServer.URL+cardURL, nil)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound || !strings.Contains(response.Header.Get("Location"), "/renditions/") {
		t.Fatalf("prepared rendition should resolve from the public endpoint: status=%d location=%s", response.StatusCode, response.Header.Get("Location"))
	}
}
