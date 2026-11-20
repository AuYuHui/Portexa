#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/resource.h>
#include <sys/stat.h>
#include <unistd.h>

#include <linux/audit.h>
#include <linux/filter.h>
#include <linux/seccomp.h>
#include <stddef.h>
#include <sys/prctl.h>
#include <sys/syscall.h>

#define ALLOW_SYSCALL(number) \
    BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, (number), 0, 1), \
    BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_ALLOW)

// Set up the image mapping before entering a worker that can only use stdio and exit.
static int install_sandbox(void) {
    struct sock_filter instructions[] = {
        BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, arch)),
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, AUDIT_ARCH_X86_64, 1, 0),
        BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_KILL_PROCESS),
        BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, nr)),
        ALLOW_SYSCALL(SYS_read),
        ALLOW_SYSCALL(SYS_write),
        ALLOW_SYSCALL(SYS_exit),
        ALLOW_SYSCALL(SYS_exit_group),
        BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_KILL_PROCESS),
    };
    const struct sock_fprog filter = {
        .len = sizeof instructions / sizeof instructions[0],
        .filter = instructions,
    };
    return prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) == 0 &&
           prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &filter, 0, 0) == 0;
}

enum {
    FileBytes = 0x77c000,
    MaxWords = 512,
    MaxJobs = 32
};

static int read_all(int fd, void *memory, size_t size) {
    unsigned char *out = memory;
    size_t consumed = 0;
    while (consumed < size) {
        ssize_t got = read(fd, out + consumed, size - consumed);
        if (got < 0 && errno == EINTR)
            continue;
        if (got <= 0)
            return got == 0 && consumed == 0 ? 0 : -1;
        consumed += (size_t)got;
    }
    return 1;
}

static int write_all(int fd, const void *memory, size_t size) {
    const unsigned char *in = memory;
    while (size != 0) {
        ssize_t put = write(fd, in, size);
        if (put < 0 && errno == EINTR)
            continue;
        if (put <= 0)
            return 0;
        in += put;
        size -= (size_t)put;
    }
    return 1;
}

#define ImageBase UINT64_C(0x100000000)
#define InputBase UINT64_C(0x200000000)
#define StateBase UINT64_C(0x210000000)

static inline uint64_t read_value(const unsigned char *image, const uint64_t *input,
                                  uint32_t count, const uint64_t *box, uint64_t address) {
    uint64_t result;
    if (address >= ImageBase && address - ImageBase <= FileBytes - 8) {
        memcpy(&result, image + (address - ImageBase), sizeof result);
    } else if (address >= InputBase && address - InputBase <= (size_t)count * 8 - 8) {
        memcpy(&result, (const unsigned char *)input + (address - InputBase), sizeof result);
    } else if (address >= StateBase && address - StateBase <= 24) {
        memcpy(&result, (const unsigned char *)box + (address - StateBase), sizeof result);
    } else {
        _Exit(15);
    }
    return result;
}

static inline void write_state(uint64_t *box, uint64_t address, uint64_t result) {
    if (address < StateBase || address - StateBase > 24)
        _Exit(16);
    memcpy((unsigned char *)box + (address - StateBase), &result, sizeof result);
}

#include "translated.inc"

int main(int argc, char **argv) {
    if (argc != 2)
        return 1;
    struct stat status;
    int fd = open(argv[1], O_RDONLY | O_CLOEXEC);
    if (fd < 0 || fstat(fd, &status) != 0 || status.st_size < FileBytes)
        return 2;
    const unsigned char *image = mmap(NULL, FileBytes, PROT_READ, MAP_PRIVATE, fd, 0);
    close(fd);
    if (image == MAP_FAILED)
        return 3;
    struct rlimit cpu = {2, 2};
    if (setrlimit(RLIMIT_CPU, &cpu) != 0)
        return 6;
    if (!install_sandbox()) {
        perror("mt5 worker seccomp");
        return 7;
    }

    const uint32_t ready = 0x01;
    if (!write_all(STDOUT_FILENO, &ready, sizeof ready))
        return 8;
    for (unsigned job = 0; job < MaxJobs; ++job) {
        uint32_t count = 0;
        int got = read_all(STDIN_FILENO, &count, sizeof count);
        if (got == 0)
            return 0;
        if (got < 0 || count < 3 || count > MaxWords)
            return 9;
        uint64_t input[MaxWords] = {0};
        if (read_all(STDIN_FILENO, input, count * sizeof *input) != 1)
            return 10;
        uint64_t result = compute(image, input, count);
        if (!write_all(STDOUT_FILENO, &result, sizeof result))
            return 11;
    }
    return 0;
}
