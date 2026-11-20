"""Translate the arithmetic routine from the supported terminal image to C."""

import re


ENTRY = 0x1000111A0
END = 0x10003FD20
CONDITIONS = {
    "b.eq": "z", "b.ne": "!z", "b.lo": "!carry", "b.cc": "!carry",
    "b.hs": "carry", "b.cs": "carry", "b.hi": "carry && !z", "b.ls": "!carry || z",
}
BRANCHES = {"b", "cbz", "cbnz", "tbz", "tbnz", *CONDITIONS}
BINARY_OPERATORS = {
    "and": "&", "orr": "|", "bic": "& ~", "orn": "| ~",
    "add": "+", "sub": "-", "lsl": "<<", "lsr": ">>",
}


def immediate(operand):
    return int(operand.lstrip("#"), 0)


def value(operand):
    if operand.startswith(("#", "0x")):
        return f"UINT64_C({immediate(operand) % (1 << 64):#x})"
    if operand in ("xzr", "wzr"):
        return "UINT64_C(0)"
    if re.fullmatch(r"x\d{1,2}", operand):
        return "r" + operand[1:]
    if re.fullmatch(r"w\d{1,2}", operand):
        return "((uint32_t)r" + operand[1:] + ")"
    raise ValueError(f"unsupported operand: {operand}")


def assign(reg, expression):
    if reg in ("xzr", "wzr"):
        return ""
    if not re.fullmatch(r"[xw]\d{1,2}", reg):
        raise ValueError(f"unsupported destination: {reg}")
    cast = "(uint32_t)" if reg.startswith("w") else ""
    return f"r{reg[1:]} = {cast}({expression});"


def addressing(operand):
    if not (operand.startswith("[") and operand.endswith("]")):
        raise ValueError(f"unsupported address: {operand}")
    base, *offset = re.split(r",\s*", operand[1:-1])
    if base == "sp":
        displacement = immediate(offset[0]) if offset else 0
        if len(offset) > 1 or not 0 <= displacement < 0x1D0 or displacement % 8:
            raise ValueError(f"unsupported stack address: {operand}")
        return "stack", displacement // 8
    if not offset:
        return "checked", value(base)
    if len(offset) == 1:
        return "checked", f"{value(base)}+{value(offset[0])}"
    if len(offset) == 2 and offset[1].startswith("lsl ") and immediate(offset[1][4:]) == 3:
        return "checked", f"{value(base)}+({value(offset[0])} << 3)"
    raise ValueError(f"unsupported index: {operand}")


def memory_instruction(op, operands):
    kind, location = addressing(operands[-1])
    if op == "ldr":
        expression = f"s{location}" if kind == "stack" else f"read_value(image,input,count,box,{location})"
        return assign(operands[0], expression)
    if op == "str":
        expression = value(operands[0])
        if kind == "stack":
            return f"s{location} = {expression};"
        return f"write_state(box,{location},{expression});"
    if kind != "stack" or location + 1 >= 58:
        raise ValueError("paired registers require two stack words")
    if op == "stp":
        return f"s{location} = {value(operands[0])}; s{location+1} = {value(operands[1])};"
    return assign(operands[0], f"s{location}") + assign(operands[1], f"s{location+1}")


def branch(op, operands):
    target = f"L{int(operands[-1], 16):x}"
    if op == "b":
        return f"goto {target};"
    if op in ("cbz", "cbnz"):
        comparison = "==" if op == "cbz" else "!="
        condition = f"{value(operands[0])} {comparison} 0"
    elif op in ("tbz", "tbnz"):
        comparison = "==" if op == "tbz" else "!="
        condition = f"(({value(operands[0])} >> {immediate(operands[1])}) & 1) {comparison} 0"
    else:
        condition = CONDITIONS[op]
    return f"if ({condition}) goto {target};"


def statement(op, operands):
    if op == "ret":
        return "return r0;"
    if op in ("sub", "add") and operands[0] == "sp":
        if len(operands) != 3 or operands[1] != "sp" or immediate(operands[2]) != 0x1D0:
            raise ValueError("unexpected stack frame")
        return ""
    if op in ("ldr", "str", "stp", "ldp"):
        return memory_instruction(op, operands)
    if op in ("mov", "mvn", "adrp"):
        invert = "~" if op == "mvn" else ""
        return assign(operands[0], invert + value(operands[1]))
    if op == "movk":
        shift = immediate(operands[2][4:]) if len(operands) > 2 else 0
        mask = ((1 << 64) - 1) ^ (0xFFFF << shift)
        written = immediate(operands[1]) << shift
        return assign(operands[0], f"({value(operands[0])} & UINT64_C({mask:#x})) | UINT64_C({written:#x})")
    if op in BINARY_OPERATORS:
        left, right = map(value, operands[1:])
        if op in ("lsl", "lsr"):
            right = f"({right} & 63)"
        return assign(operands[0], f"{left} {BINARY_OPERATORS[op]} {right}")
    if op in ("cmp", "cmn", "tst", "bics"):
        left, right = map(value, operands[-2:])
        if op == "cmp":
            return f"z=({left}=={right}); carry=({left}>={right});"
        if op == "cmn":
            return f"z=(({left}+{right})==0); carry=({left}>UINT64_MAX-{right});"
        if op == "bics" and operands[0] != "xzr":
            raise ValueError("unsupported bit test destination")
        mask = "~" + right if op == "bics" else right
        return f"z=(({left}&{mask})==0); carry=0;"
    if op == "cset" and operands[-1] == "ne":
        return assign(operands[0], "!z")
    if op in BRANCHES:
        return branch(op, operands)
    if op in ("madd", "msub"):
        left, right, addend = map(value, operands[1:])
        operator = "+" if op == "madd" else "-"
        return assign(operands[0], f"{addend} {operator} ({left}*{right})")
    if op == "umulh":
        left, right = map(value, operands[1:])
        return assign(operands[0], f"(uint64_t)(((__uint128_t){left} * {right}) >> 64)")
    raise ValueError(f"unsupported instruction: {op}")


def generate(disassembly, destination):
    instructions = []
    instruction = re.compile(r"^\s*([0-9a-f]+):[ \t]+([.\w]+)(?:[ \t]+(.*))?$", re.M)
    for address, op, args in instruction.findall(disassembly):
        operands = args.split(";", 1)[0].split(" //", 1)[0].split(" <", 1)[0].strip()
        parts = re.split(r",\s*(?![^\[]*\])", operands) if operands else []
        instructions.append((int(address, 16), op, parts))
    if [item[0] for item in instructions] != list(range(ENTRY, END, 4)):
        raise ValueError("disassembler did not return the full supported routine")
    branch_targets = {int(args[-1], 16) for _, op, args in instructions if op in BRANCHES}
    if any(target not in range(ENTRY, END, 4) for target in branch_targets):
        raise ValueError("branch leaves the supported routine")

    lines = []
    for address, op, operands in instructions:
        if address in branch_targets:
            lines.append(f"L{address:x}:;")
        try:
            code = statement(op, operands)
        except (ValueError, IndexError) as err:
            raise ValueError(f"instruction at {address:#x}: {err}") from err
        if code:
            lines.append(code)

    blocks = destination / "translated"
    blocks.mkdir()
    includes = []
    for offset in range(0, len(lines), 650):
        name = f"block_{offset//650:03}.inc"
        (blocks / name).write_text("\n".join(lines[offset:offset + 650]) + "\n")
        includes.append(f'#include "translated/{name}"')
    inputs = {0: "StateBase", 1: "InputBase", 2: "count"}
    registers = ", ".join(f"r{i}={inputs.get(i, '0')}" for i in range(31))
    stack = ", ".join(f"s{i}=0" for i in range(58))
    include_lines = "\n".join(includes)
    (destination / "translated.inc").write_text(f"""static uint64_t compute(const unsigned char *image, const uint64_t *input, uint32_t count) {{
    uint64_t box[4] = {{0}};
    uint64_t {registers};
    uint64_t {stack};
    int z=0, carry=0;
{include_lines}
}}
""")
