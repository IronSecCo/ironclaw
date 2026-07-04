package isolation

import "strings"

// IsImageMissing reports whether a launch error was caused by the sandbox
// container image not being present. It is a best-effort string classifier over
// the errors the concrete isolators return: the Docker Engine API answers a create
// against an absent image with a 404 "No such image: <ref>", and the containerd/OCI
// path surfaces "not found"/"manifest unknown" when the image was never pulled.
//
// The daemon uses it (via session.Config.OnLaunchError) to turn a launch failure
// into a specific, actionable chat message ("build the sandbox image") rather than a
// silent empty reply (IRO-335). A false result means "some other launch fault", for
// which the caller shows a generic message.
func IsImageMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no such image"):
		return true
	case strings.Contains(msg, "manifest unknown"):
		return true
	case strings.Contains(msg, "image") && strings.Contains(msg, "not found"):
		return true
	default:
		return false
	}
}
