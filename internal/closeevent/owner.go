package closeevent

// UnknownSession labels a close event whose owning session cannot be
// determined. Such an event is still restorable — the sub-manifest carries the
// session name it will be restored into — it just cannot be grouped.
const UnknownSession = "(unknown session)"

// OwnerSession reports which tmux session a close event belonged to.
//
// The stored name comes first: it was captured at hook time, so it is correct
// even for a session that no longer exists. The snapshot diff is second, which
// covers events recorded before the name was stored and every after-kill-pane
// event (a command hook carries no hook_* formats).
//
// The event's session_id is deliberately never consulted. tmux restarts session
// ids at $0 with every server, so a stale id can collide with a live session's
// and attribute a close to the wrong one. Unknown beats wrong.
func OwnerSession(post CloseManifest, item *ClosedItem) string {
	if post.SessionName != "" {
		return post.SessionName
	}
	if item != nil && item.SessionName != "" {
		return item.SessionName
	}
	return UnknownSession
}
