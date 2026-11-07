//go:build cgo && linux

package player

/*
#include <stdlib.h>
#if defined(__GLIBC__)
#include <malloc.h>
#endif

// Runs while the process loads, before Go package init and before any worker
// threads can create their own malloc arenas. A Go init is too late: the C
// runtime already had several arenas by then (docs/acceptance.md, 2026-10-18).
// A valid MALLOC_ARENA_MAX in the environment wins so the uncapped layout stays
// reproducible for the acceptance comparison; glibc ignores invalid values, so
// mirror that and fall back to our limit.
__attribute__((constructor))
static void gd_limit_malloc_arenas(void) {
#if defined(__GLIBC__)
	const char *value = getenv("MALLOC_ARENA_MAX");
	if (value != NULL && atoi(value) > 0)
		return;
	mallopt(M_ARENA_MAX, 2);
#endif
}
*/
import "C"
