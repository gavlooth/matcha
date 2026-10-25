package app

func (m *Model) composerHasVimFocus() bool {
	composer := getCurrentComposer(m.current)
	return composer != nil && composer.IsVimEditing()
}
