// SPDX-License-Identifier: GPL-3.0-or-later

package i18n

// ruCLI is the Russian translation of the command-line interface; the
// cli package's coverage test fails if a message is missing here.
var ruCLI = map[string]string{
	`usage: tuntop [command]

  (no command)         interactive interface (TUI); status when not a terminal
  status               connection state
  on [server]          connect: to the given server, or the last one used
  off                  disconnect; traffic goes directly
  use <server>         same as "on <server>"
  servers              list servers of all subscriptions
  test [server]        measure latency (all servers by default)
  killswitch [on|off]  block traffic outside the tunnel while the VPN is on
  fav <server>         mark a server as favorite: it is listed first
  unfav <server>       unmark a favorite
  sub                  list subscriptions
  sub add <url|->      add a subscription ("-" reads the URL from stdin)
         [-ua UA] [-no-hwid]
  sub rm <n|id>        remove a subscription
  sub mv <n|id> <pos>  move a subscription to a position in the list
  update [n|id]        refresh subscriptions now (all by default)
  add <link…|->        add individual vless:// links
  rm <server>          remove an individually added link
  notify               show desktop notifications about the VPN (runs until stopped)
  version              print version

<server> is a number from "tuntop servers" or part of the server name.
`: `использование: tuntop [команда]

  (без команды)        интерфейс (TUI); не в терминале — состояние
  status               состояние подключения
  on [сервер]          включить VPN: через указанный сервер или последний
  off                  выключить VPN; трафик пойдёт напрямую
  use <сервер>         то же, что "on <сервер>"
  servers              серверы всех подписок
  test [сервер]        замер задержки (по умолчанию всех серверов)
  killswitch [on|off]  блокировать трафик мимо туннеля, пока VPN включён
  fav <сервер>         добавить сервер в избранное: он будет в начале списка
  unfav <сервер>       убрать сервер из избранного
  sub                  подписки
  sub add <ссылка|->   добавить подписку ("-" — прочитать ссылку из stdin)
         [-ua UA] [-no-hwid]
  sub rm <n|id>        удалить подписку
  sub mv <n|id> <поз>  переставить подписку на позицию в списке
  update [n|id]        обновить подписки сейчас (по умолчанию все)
  add <ссылка…|->      добавить отдельные vless:// ссылки
  rm <сервер>          удалить отдельно добавленную ссылку
  notify               уведомления рабочего стола о VPN (работает до остановки)
  version              версия

<сервер> — номер из "tuntop servers" или часть названия сервера.
`,
	"unknown command %q": "неизвестная команда %q",
	"start it with:":     "запустите:",
	"connecting…":        "подключаюсь…",
	"no servers yet: tuntop sub add <subscription URL>, or tuntop add <vless://…>": "серверов пока нет: tuntop sub add <ссылка подписки> или tuntop add <vless://…>",
	"connect: tuntop on <number>":                                              "подключиться: tuntop on <номер>",
	"measuring latency…":                                                       "замеряю задержку…",
	"no server matches %q":                                                     "нет сервера, подходящего под %q",
	"%q matches %d servers, be more specific or use the number":                "под %q подходят серверы (%d) — уточните или укажите номер",
	"User-Agent for this subscription (default: Happ-compatible)":              "User-Agent для этой подписки (по умолчанию совместимый с Happ)",
	"do not send the device id":                                                "не отправлять идентификатор устройства",
	"downloading…":                                                             "загружаю…",
	"sub: unknown operation %q (ls, add, rm, mv)":                              "sub: неизвестная операция %q (ls, add, rm, mv)",
	`sub mv: expected a subscription and a position, e.g. "tuntop sub mv 2 1"`: `sub mv: нужны подписка и позиция, например "tuntop sub mv 2 1"`,
	"sub mv: bad position %q":                                                  "sub mv: неверная позиция %q",
	`which server? See "tuntop servers"`:                                       `какой сервер? См. "tuntop servers"`,
	"★ %s: in favorites, listed first\n":                                       "★ %s: в избранном, в начале списка\n",
	"%s: removed from favorites\n":                                             "%s: убран из избранного\n",
	"kill:    on, traffic outside the tunnel is blocked\n":                     "защита:  включена, трафик мимо туннеля блокируется\n",
	"BLOCKED: kill switch is on and the VPN is down: tuntop on <server>, or tuntop off for direct traffic\n": "БЛОКИРОВКА: защита включена, а VPN упал: tuntop on <сервер> или tuntop off — трафик напрямую\n",
	"subscription not added":         "подписка не добавлена",
	"killswitch: expected on or off": "killswitch: укажите on или off",
	"kill switch: on; while the VPN is on, traffic goes only through the tunnel\n": "защита (kill switch): включена; пока VPN включён, трафик идёт только через туннель\n",
	"kill switch: off\n":                                       "защита (kill switch): выключена\n",
	"★ Favorites":                                              "★ Избранное",
	"no subscriptions: tuntop sub add <URL>":                   "подписок нет: tuntop sub add <ссылка>",
	"provider: %s":                                             "провайдер: %s",
	"subscription number %d out of range 1..%d":                "номер подписки %d вне диапазона 1..%d",
	"updating…":                                                "обновляю…",
	"added. See them with: tuntop servers":                     "добавлено. Список: tuntop servers",
	`use: which server? See "tuntop servers"`:                  `use: какой сервер? Список: "tuntop servers"`,
	`sub add: expected one URL (or "-" to read it from stdin)`: `sub add: нужна одна ссылка (или "-", чтобы прочитать её из stdin)`,
	`sub rm: which one? Give its number from "tuntop sub"`:     `sub rm: какую? Укажите номер из "tuntop sub"`,
	`add: give one or more vless:// links (or "-" for stdin)`:  `add: укажите одну или несколько vless:// ссылок (или "-" для stdin)`,
	`rm: no server %q (use its number from "tuntop servers")`:  `rm: нет сервера %q (укажите номер из "tuntop servers")`,

	"VPN:     ON, all traffic goes through the server\n": "VPN:     ВКЛЮЧЁН, весь трафик идёт через сервер\n",
	"server:  %s\n":                                                      "сервер:  %s\n",
	"traffic: ↓ %s (%s)  ↑ %s (%s)\n":                                    "трафик:  ↓ %s (%s)  ↑ %s (%s)\n",
	"since:   %s (%s)\n":                                                 "с:       %s (%s)\n",
	"VPN:     off, traffic goes directly\n":                              "VPN:     выключен, трафик идёт напрямую\n",
	"on:      \"tuntop on\" connects to %s\n":                            "вкл.:    \"tuntop on\" подключит к %s\n",
	"on:      pick a server first: tuntop servers, then tuntop on <n>\n": "вкл.:    сначала выберите сервер: tuntop servers, затем tuntop on <n>\n",
	"error:   %s\n":                                                      "ошибка:  %s\n",
	"VPN on: %s\n":                                                       "VPN включён: %s\n",
	"added %s: %d servers. See them with: tuntop servers\n":              "добавлена %s: серверов %d. Список: tuntop servers\n",

	// notifications
	"VPN on":                    "VPN включён",
	"VPN off":                   "VPN выключен",
	"Traffic goes directly":     "Трафик идёт напрямую",
	"VPN did not connect":       "VPN не подключился",
	"Subscription not updated":  "Подписка не обновилась",
	"Subscription expires soon": "Подписка скоро закончится",
	"%s: %d days left":          "%s: осталось дней: %d",
}

func init() {
	for k, v := range ruCLI {
		ru[k] = v
	}
}
