package auth

import (
	"testing"

	"websiteengine/pkg/environment"
)

func TestContentAndSiteManagementRoleMatrix(t *testing.T) {
	for _, role := range []string{"admin", "moderator", "staff", "user"} {
		if !CanPublishContent(role) || !CanManageMedia(role) || !CanViewSubmissions(role) || !CanManageSubmissions(role) {
			t.Errorf("role %q should have access to content work", role)
		}
		if !CanRollbackContent(role, environment.Dev) || !CanUnpublishContent(role, environment.Dev) {
			t.Errorf("role %q should be able to manage dev content", role)
		}
	}
	for _, role := range []string{"admin", "moderator"} {
		if !CanManageFeatures(role) || !CanPromoteContent(role) || !CanRollbackContent(role, environment.Prod) || !CanUnpublishContent(role, environment.Prod) {
			t.Errorf("role %q should manage site settings and promote content", role)
		}
	}
	for _, role := range []string{"staff", "user"} {
		if CanManageFeatures(role) || CanPromoteContent(role) || CanRollbackContent(role, environment.Prod) || CanUnpublishContent(role, environment.Prod) {
			t.Errorf("role %q must not manage site settings or promote content", role)
		}
		if CanDeleteMedia(role) {
			t.Errorf("role %q must not permanently delete shared media", role)
		}
	}
	if !CanDeleteMedia("admin") || CanDeleteMedia("moderator") {
		t.Fatal("only admin can permanently delete shared media")
	}
}
