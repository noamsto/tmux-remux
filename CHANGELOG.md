# Changelog

## [0.7.0](https://github.com/noamsto/tmux-remux/compare/v0.6.0...v0.7.0) (2026-09-22)


### Features

* **restore:** capture and replay window and pane decoration options ([#155](https://github.com/noamsto/tmux-remux/issues/155)) ([7ac59b6](https://github.com/noamsto/tmux-remux/commit/7ac59b6c72c6ba268c8dd0cfb2207614b8f525d6))

## [0.6.0](https://github.com/noamsto/tmux-remux/compare/v0.5.0...v0.6.0) (2026-09-20)


### Features

* **picker:** column grid for the close list row layout ([#151](https://github.com/noamsto/tmux-remux/issues/151)) ([229b681](https://github.com/noamsto/tmux-remux/commit/229b6817b5713e27d3f9c1eb28b3febd30d68412))

## [0.5.0](https://github.com/noamsto/tmux-remux/compare/v0.4.0...v0.5.0) (2026-09-18)


### ⚠ BREAKING CHANGES

* rename project tmux-state → tmux-remux ([#45](https://github.com/noamsto/tmux-remux/issues/45))

### Features

* agent resume-on-restore for Claude & Codex ([#49](https://github.com/noamsto/tmux-remux/issues/49)) ([ad50bbf](https://github.com/noamsto/tmux-remux/commit/ad50bbf8462ca3edd84f0ce01748493463e4c4a9))
* **demo:** add a snapshot-picker recording ([#83](https://github.com/noamsto/tmux-remux/issues/83)) ([701fcf5](https://github.com/noamsto/tmux-remux/commit/701fcf5a3e731902edf6f2783b6c6e23246fda1c))
* **picker:** consume themestate module for flavour detection ([#99](https://github.com/noamsto/tmux-remux/issues/99)) ([17ce4c5](https://github.com/noamsto/tmux-remux/commit/17ce4c5009f19d88c88d599f65f68b46310381d8))
* **picker:** draw the window's pane layout as a map ([#91](https://github.com/noamsto/tmux-remux/issues/91)) ([#93](https://github.com/noamsto/tmux-remux/issues/93)) ([06b78a7](https://github.com/noamsto/tmux-remux/commit/06b78a7813b462da6bcf9b1377d581e9e4690579))
* **picker:** full Alt+hjkl hint, and mouse wheel + click routing ([#137](https://github.com/noamsto/tmux-remux/issues/137)) ([affe179](https://github.com/noamsto/tmux-remux/commit/affe179c67b087aa02299d842da87d88d7694bd0))
* **picker:** glyph the close list's scope column and cap the preview ([#124](https://github.com/noamsto/tmux-remux/issues/124)) ([b721f89](https://github.com/noamsto/tmux-remux/commit/b721f89f4821d6e3fbb3512cdee98ce8828f0fa1))
* **picker:** hide unrecoverable closes behind a count line ([#54](https://github.com/noamsto/tmux-remux/issues/54)) ([a8935d2](https://github.com/noamsto/tmux-remux/commit/a8935d270cf1fe109baeb1c8c9802d08b5a43b1e))
* **picker:** make the close picker legible ([#103](https://github.com/noamsto/tmux-remux/issues/103)) ([7c60986](https://github.com/noamsto/tmux-remux/commit/7c609867f2253962ff79e80b3ce51ddfb6dc60b0))
* **picker:** name save reasons in words, and record the close picker ([#89](https://github.com/noamsto/tmux-remux/issues/89)) ([e451f52](https://github.com/noamsto/tmux-remux/commit/e451f522237f5c2d2c69474d0b3130aeb58142a5))
* **picker:** number tree panes and show a mini-map over scrollback ([#97](https://github.com/noamsto/tmux-remux/issues/97)) ([fe55a9e](https://github.com/noamsto/tmux-remux/commit/fe55a9e81b48af06af2c9937b5bd3005e6df163a))
* **picker:** preview a closed pane's scrollback in close mode ([#87](https://github.com/noamsto/tmux-remux/issues/87)) ([6c698ac](https://github.com/noamsto/tmux-remux/commit/6c698ac8901678aaee84fee5f67db7c1be816741))
* **picker:** rebuild the close picker as a flat list with a scrollback preview ([#106](https://github.com/noamsto/tmux-remux/issues/106)) ([38795b5](https://github.com/noamsto/tmux-remux/commit/38795b52b0327b8e8ec6e809310c386b841961e2))
* **picker:** rule off the close list's other-sessions section ([#138](https://github.com/noamsto/tmux-remux/issues/138)) ([cde7b8d](https://github.com/noamsto/tmux-remux/commit/cde7b8de59c5126c8de86543604751426366292d))
* render tmux triggers from Go, gated on tmux 3.8 ([#65](https://github.com/noamsto/tmux-remux/issues/65)) ([fecdd2d](https://github.com/noamsto/tmux-remux/commit/fecdd2d0eff4171a53da42a1d3f14ed23d2812e0))


### Bug Fixes

* **bridge:** read the live [@bridge](https://github.com/bridge)_host, and skip mirrors on restore ([#128](https://github.com/noamsto/tmux-remux/issues/128)) ([5ca98f5](https://github.com/noamsto/tmux-remux/commit/5ca98f52054c38ef58fe774d32d226213e1b461a))
* **closeevent:** adopt a pane's last capture when its close snapshot skipped scrollback ([#125](https://github.com/noamsto/tmux-remux/issues/125)) ([9ee091c](https://github.com/noamsto/tmux-remux/commit/9ee091c1f963e1b84f2724fb1d5ba9aecc2e6916))
* **closeevent:** drop closes inside bridge-mirror sessions ([#120](https://github.com/noamsto/tmux-remux/issues/120)) ([1a2e9df](https://github.com/noamsto/tmux-remux/commit/1a2e9df30e99701fbb48a80e4d0201e0a23f2974)), closes [#119](https://github.com/noamsto/tmux-remux/issues/119)
* **closeevent:** embed the resolved entity at capture time ([#112](https://github.com/noamsto/tmux-remux/issues/112)) ([427a056](https://github.com/noamsto/tmux-remux/commit/427a056ea77d7dc33abbd8119dab797bdd32cda9))
* **demo:** move the key badge to the bottom, defer the action behind it ([#76](https://github.com/noamsto/tmux-remux/issues/76)) ([0e43c88](https://github.com/noamsto/tmux-remux/commit/0e43c880b2af39fbb500a8a3636365b18b998098))
* **demo:** stop fish autosuggestion leaking into the recording, add a key overlay ([#75](https://github.com/noamsto/tmux-remux/issues/75)) ([adedaab](https://github.com/noamsto/tmux-remux/commit/adedaab006fd59d8d1782917791b6794f7135e8c))
* **demo:** stop fish reprinting truncated prompts in the recording ([#77](https://github.com/noamsto/tmux-remux/issues/77)) ([8e8a201](https://github.com/noamsto/tmux-remux/commit/8e8a20102d6b787d0786d0de7e6670c512b8d251))
* drop restored relaunch panes to a shell on exit ([#55](https://github.com/noamsto/tmux-remux/issues/55)) ([349d2ed](https://github.com/noamsto/tmux-remux/commit/349d2ed82676bf2f45cdbfbfc50473afb6fcdbca))
* harden restore/save paths and picker layout from audit findings ([#50](https://github.com/noamsto/tmux-remux/issues/50)) ([648c22f](https://github.com/noamsto/tmux-remux/commit/648c22f6f6893771cb2a5d1bda9c871b3f5a9eeb))
* include [@remux](https://github.com/remux)_relaunch in the structure fingerprint ([#74](https://github.com/noamsto/tmux-remux/issues/74)) ([ea13a33](https://github.com/noamsto/tmux-remux/commit/ea13a330b30126be8f91b568179fa450e09bf480))
* make undo work for entities closed soon after they were created ([#57](https://github.com/noamsto/tmux-remux/issues/57)) ([40cc675](https://github.com/noamsto/tmux-remux/commit/40cc67592ccb0c336182e6828d464d7dc8b117be))
* **picker:** align the theme with lazytmux and prdash ([#141](https://github.com/noamsto/tmux-remux/issues/141)) ([6f735dd](https://github.com/noamsto/tmux-remux/commit/6f735dd7b82297ad242a9fae0245c032504d3488))
* **picker:** draw the close list's scope column in Nerd Font ([#136](https://github.com/noamsto/tmux-remux/issues/136)) ([e6089fc](https://github.com/noamsto/tmux-remux/commit/e6089fc138f940436bd357bcde51779c51259344))
* **picker:** drop --session from the close-picker display-popup bind ([#100](https://github.com/noamsto/tmux-remux/issues/100)) ([d8dc3a4](https://github.com/noamsto/tmux-remux/commit/d8dc3a46f7250bfcf746cbe5df9f93f5e8379114)), closes [#90](https://github.com/noamsto/tmux-remux/issues/90)
* **picker:** explain empty preview and filtered-out session contents ([#101](https://github.com/noamsto/tmux-remux/issues/101)) ([ce23d04](https://github.com/noamsto/tmux-remux/commit/ce23d04161e6acb76966fa71c57bd0ecd3790b17)), closes [#86](https://github.com/noamsto/tmux-remux/issues/86)
* **picker:** give the close row's command its own colour ([#115](https://github.com/noamsto/tmux-remux/issues/115)) ([8a916ee](https://github.com/noamsto/tmux-remux/commit/8a916ee1742c6a740f122b36e1faba89f46f2e42))
* **picker:** pane map labels, window-node nav, and demo key casts ([#96](https://github.com/noamsto/tmux-remux/issues/96)) ([671bd4c](https://github.com/noamsto/tmux-remux/commit/671bd4c017cfa0d1aa89954217b6864274aa6f9b))
* **picker:** propagate scrollback-skipped to the close preview ([#113](https://github.com/noamsto/tmux-remux/issues/113)) ([0f7be1a](https://github.com/noamsto/tmux-remux/commit/0f7be1ab012c743f53f2cfbd3e81c6e050fa7ad1)), closes [#111](https://github.com/noamsto/tmux-remux/issues/111)
* **restore:** create a session and its first window in one call ([#58](https://github.com/noamsto/tmux-remux/issues/58)) ([04c7e82](https://github.com/noamsto/tmux-remux/commit/04c7e82b1d101e1a18477d7447c8dd5afd5632b1))
* **restore:** drop floating panes from undo ([#132](https://github.com/noamsto/tmux-remux/issues/132)) ([2da0496](https://github.com/noamsto/tmux-remux/commit/2da04965665b747acaeb583785bda2f103b1fad1))
* **snapshot:** skip sessions carrying [@bridge](https://github.com/bridge)_host ([#108](https://github.com/noamsto/tmux-remux/issues/108)) ([0b0ebf6](https://github.com/noamsto/tmux-remux/commit/0b0ebf6c430112469ae74ec882c653575434d6b4)), closes [#107](https://github.com/noamsto/tmux-remux/issues/107)
* **store:** add id DESC tiebreak to snapshot/event ordering ([#98](https://github.com/noamsto/tmux-remux/issues/98)) ([49537f7](https://github.com/noamsto/tmux-remux/commit/49537f78c9d8209f8fc9ce6ad7578a7bdf4d0c71)), closes [#66](https://github.com/noamsto/tmux-remux/issues/66)
* **store:** partition state.db by tmux server ([#130](https://github.com/noamsto/tmux-remux/issues/130)) ([#134](https://github.com/noamsto/tmux-remux/issues/134)) ([3fa057d](https://github.com/noamsto/tmux-remux/commit/3fa057d6eebf274664d9fb0d47b57098c8d06857))
* **store:** prune close events that outlive their snapshots ([#110](https://github.com/noamsto/tmux-remux/issues/110)) ([3c53bf3](https://github.com/noamsto/tmux-remux/commit/3c53bf394edb86f5e8e645dca9677e2c89069677))
* **tmux:** tolerate the empty pane_pid a dead pane reports ([#114](https://github.com/noamsto/tmux-remux/issues/114)) ([c064481](https://github.com/noamsto/tmux-remux/commit/c06448173080e53d699d199124f436476e446b3a))
* **triggers:** drop the invalid session scope from the 3.9 monitor hook ([#143](https://github.com/noamsto/tmux-remux/issues/143)) ([60c1ccc](https://github.com/noamsto/tmux-remux/commit/60c1cccba27d17276bd14af2f3cb7556bd8f9663))


### Code Refactoring

* rename project tmux-state → tmux-remux ([#45](https://github.com/noamsto/tmux-remux/issues/45)) ([3e437a2](https://github.com/noamsto/tmux-remux/commit/3e437a2ba28fc5cdcb1cf40a4557fe2e005c4a0a))
