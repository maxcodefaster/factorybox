#ifndef RUNTIME_FORK_COW_PIN_GUARD_H
#define RUNTIME_FORK_COW_PIN_GUARD_H

#include <errno.h>
#include <stddef.h>
#include <stdint.h>
#include <unistd.h>

#define COW_PIN_PAGE_SIZE 4096UL
#define COW_PIN_SCAN_PAGES 128

enum cow_pin_state {
	COW_PIN_SHARED = 0,
	COW_PIN_NOT_PRESENT = 1,
	COW_PIN_DIFFERENT = 2,
	COW_PIN_UNAVAILABLE = 3,
};

static inline int cow_pin_classify(uint64_t source, uint64_t holder)
{
	const uint64_t present = 1ULL << 63, swapped = 1ULL << 62;
	const uint64_t exclusive = 1ULL << 56, pfn = (1ULL << 55) - 1;
	if (!(source & present) || !(holder & present) || ((source | holder) & swapped))
		return COW_PIN_NOT_PRESENT;
	if (!(source & pfn) || !(holder & pfn))
		return COW_PIN_UNAVAILABLE;
	if ((source & pfn) != (holder & pfn) || ((source | holder) & exclusive))
		return COW_PIN_DIFFERENT;
	return COW_PIN_SHARED;
}

static inline ssize_t cow_pin_pread(int fd, void *data, size_t bytes, off_t offset)
{
	ssize_t result;
	do {
		result = pread(fd, data, bytes, offset);
	} while (result < 0 && errno == EINTR);
	return result;
}

// Only an exact, present, nonexclusive physical page shared with the stopped
// helper proves that the source's current page received fork CoW protection.
// Unknown PFNs and pages populated independently after fork require a copy.
static inline int cow_pin_shared_prefix(int source, int holder, uint64_t address,
				       size_t bytes, size_t *length, int *state)
{
	uint64_t original[COW_PIN_SCAN_PAGES], protected[COW_PIN_SCAN_PAGES];
	size_t pages = bytes / COW_PIN_PAGE_SIZE;
	size_t count, prefix;
	off_t offset;
	if (!bytes || bytes % COW_PIN_PAGE_SIZE || address % COW_PIN_PAGE_SIZE ||
	    address / COW_PIN_PAGE_SIZE > INT64_MAX / sizeof(uint64_t))
		return -1;
	if (pages > COW_PIN_SCAN_PAGES)
		pages = COW_PIN_SCAN_PAGES;
	count = pages * sizeof(uint64_t);
	offset = (address / COW_PIN_PAGE_SIZE) * sizeof(uint64_t);
	if (cow_pin_pread(source, original, count, offset) != (ssize_t)count ||
	    cow_pin_pread(holder, protected, count, offset) != (ssize_t)count) {
		*length = pages * COW_PIN_PAGE_SIZE;
		*state = COW_PIN_UNAVAILABLE;
		return 0;
	}
	*state = cow_pin_classify(original[0], protected[0]);
	prefix = 1;
	while (prefix < pages && cow_pin_classify(original[prefix], protected[prefix]) == *state)
		prefix++;
	*length = prefix * COW_PIN_PAGE_SIZE;
	return 0;
}

#endif
