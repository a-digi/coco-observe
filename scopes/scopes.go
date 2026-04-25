package scopes

// Scopes defined by coco-observe. Consumers import these constants
// and register them with their own permission/role system.
// New scopes are added here; the consumer decides which roles receive them.
const (
	// ScopeView allows reading metrics and agent status.
	ScopeView = "observe:view"

	// ScopeManage allows creating, editing, and deleting agents
	// and generating API credentials.
	ScopeManage = "observe:manage"

	// ScopeArchive allows querying archived (historical) databases.
	ScopeArchive = "observe:archive"
)
