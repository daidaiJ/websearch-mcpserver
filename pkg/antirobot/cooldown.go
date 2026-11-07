package antirobot

import "time"

// 服务端限流冷却的档位升级规则，此前在 ddg / arxiv / github 等处各写一份
// （读档位、判断、写回跨锁边界）。收敛为纯函数后，调用方只需在自己的临界区内
// 读写档位，避免并发进入冷却时后写者用旧档位覆盖先写者已升级的冷却。
//
// NextCooldownDuration 计算本次冷却时长：
//   - Retry-After 明确给出等待时长时以其为准，并封顶到 max（防异常大值）；
//   - 当前档位 <= 0 时取 base，否则翻倍；再封顶到 max。
//
// current 为引擎当前档位（0 表示首次限流），base 为首次冷却时长，
// max 为冷却上限，retryAfter 为解析后的 Retry-After（0 表示未给出）。
func NextCooldownDuration(current, base, max, retryAfter time.Duration) time.Duration {
	cd := current
	if cd <= 0 {
		cd = base
	} else {
		cd *= 2
	}
	if cd > max {
		cd = max
	}
	if retryAfter > 0 {
		if retryAfter > max {
			retryAfter = max
		}
		cd = retryAfter
	}
	return cd
}
