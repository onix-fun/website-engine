package auth

import (
	"websiteengine/pkg/environment"
	"websiteengine/pkg/principal"
)

// Capabilities are the server-side role checks shared by HTTP handlers. They
// intentionally mirror the role matrix documented in README.md.

func CanManageSchema(role string) bool { return IsStaff(role) }

func CanManageFeatures(role string) bool { return IsStaff(role) }

func CanPublishContent(role string) bool { return ValidRole(role) }

func CanPromoteContent(role string) bool { return IsStaff(role) }

func CanUnpublishContent(role string, env environment.Environment) bool {
	if env == environment.Dev {
		return ValidRole(role)
	}
	return IsStaff(role)
}

func CanRollbackContent(role string, env environment.Environment) bool {
	if env == environment.Dev {
		return IsStaff(role)
	}
	return IsAdmin(role)
}

func CanManageMedia(role string) bool { return ValidRole(role) }

func CanDeleteMedia(role string) bool { return IsAdmin(role) }

func CanViewSubmissions(role string) bool { return ValidRole(role) }

func CanManageSubmissions(role string) bool { return IsStaff(role) }

func CanImportReleases(role string) bool { return IsStaff(role) }

func CanActivateRelease(role string, env environment.Environment) bool {
	if env == environment.Dev {
		return IsStaff(role)
	}
	return IsAdmin(role)
}

func CanCreateFeature(role string) bool {
	r := principal.Role(role)
	return r == principal.RoleAdmin || r == RoleModerator || r == principal.RoleStaff || r == principal.Role("admin") || r == principal.Role("staff")
}
