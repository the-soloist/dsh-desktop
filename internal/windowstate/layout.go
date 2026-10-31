package windowstate

import "errors"

const MaximumColumnWidth = 10000

var ErrInvalidColumnWidth = errors.New("invalid sidebar column or width")

// LoadLayout returns the user-selected column widths independently of geometry.
func (store *Store) LoadLayout() Layout { return store.Snapshot().Layout }

// SaveWidth updates one preference and preserves all other window state.
func (store *Store) SaveWidth(side string, width int) error {
	if width < 1 || width > MaximumColumnWidth || (side != "sidebar" && side != "rightbar") {
		return ErrInvalidColumnWidth
	}
	store.Update(func(state *State) {
		if side == "sidebar" {
			state.Layout.SidebarWidth = width
		} else {
			state.Layout.RightbarWidth = width
		}
	})
	return store.Save()
}
