# Architecture Rules

Godrome stays a single process: MyGO UI, in-process libmpv, no server, no database. These rules are the target. Current code does not satisfy them yet. Migrate toward them; do not add new violations.

## Dependency direction

```text
main
 ↓
app
 ↓
session / playback / library
 ↓
internal services (subsonic, player, cover, cache, config)
```

- `main` binds the window, flags, and settings file. It does not contain page or playback rules.
- `app` holds the main-thread snapshot and renders views. It translates input into commands.
- `session` owns login, request generation, cancellation, album page cache, and search debounce.
- `playback` owns the queue, repeat mode, generation, and transport commands.
- `library` owns track and album values used by the queue and views.
- `internal/subsonic` speaks the protocol. `internal/player` owns libmpv. `internal/cover` owns fetch, decode, and the bitmap budget. `internal/cache` owns the bounded disk store for cover bytes and metadata. `internal/config` owns the settings whitelist.

Forbidden:

- UI directly controls the playback engine
- UI handles request generation
- UI owns pagination logic
- `subsonic` types escape the client boundary
- views reading or writing another view's fields to decide behavior
- acceptance or tests driving unexported `app` fields

`player.Engine` stays behind the playback controller. Views call `Play`, `Previous`, `Next`, `TogglePause`, `Seek`, `Stop`, and `CycleRepeat`. They do not call `Load`, bump generation, or advance the queue on EOF.

## State

Application state must have one owner.

| State | Owner |
|---|---|
| Screen | `session` |
| Request generation and in-flight cancel | `session` |
| Album page cache | `session` |
| Queue, repeat, playback generation | `playback` |
| Engine position and engine state | `player` |
| Cover bytes and decoded bitmaps | `cover` |
| Cover and metadata disk cache | `cache` |
| Server, username, volume, window size, cache limit | `config` |

`app` may cache a snapshot for the current frame. It does not keep a second copy of the rules.

Forbidden:

- page string comparisons
- title-based state detection (`albumName == "曲库"` and the same pattern)
- duplicate repeat state (`int` plus `repeatMode`)
- using `player.State` string values as UI copy
- local files modeled as `subsonic.Song`

A screen is one value: `login`, `library`, `albums`, `album`, `search`, `starred`, `playlists`, or `local`. Empty search returns to the library.

## Refactoring rules

Every migration must:

- keep existing behavior
- keep tests passing
- avoid unrelated cleanup
- migrate one responsibility at a time

Do not extract an interface unless a second adapter already exists. `playback` has one. Subsonic and cover do not.

Order:

1. Screen value, then fix empty search.
2. Repeat and EOF advancement into `playback`.
3. `player.State` as an enum; Chinese labels stay in the view.
4. Request generation and pagination into `session`.
5. One window-binding helper for `main` and acceptance.
6. `library.Track` split from `subsonic.Song`.

`make build check test` passes after each step. Steps that change playback also run `make test-audio`. Report silent output, UI tester, and non-cgo builds separately.
