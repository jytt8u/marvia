package client

import mrand "math/rand/v2"

// PickServerName выбирает имя прикрытия так же, как дозвон под REALITY.
// Только для тестов: сам выбор живёт внутри замыкания дозвона, и добраться до
// него иначе можно было бы лишь подняв настоящую ноду.
func PickServerName(n Node) string {
	names := n.serverNames(n.SNI)
	return names[mrand.IntN(len(names))]
}
