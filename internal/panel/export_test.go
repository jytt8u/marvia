package panel

// SetReleaseURL подменяет адрес последнего релиза на время проверки.
func SetReleaseURL(u string) (restore func()) {
	was := releaseURL
	releaseURL = u
	return func() { releaseURL = was }
}
