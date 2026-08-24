//go:build !unix

package tunbridge

// CloseFD на настольных системах не делает ничего: дескриптор интерфейса
// приходит только от VpnService, а это Android, то есть Linux. Заглушка нужна
// лишь для того, чтобы пакет собирался под Windows при обычной проверке.
func CloseFD(int) {}
