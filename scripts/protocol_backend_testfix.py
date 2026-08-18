from pathlib import Path

ZERO_ROOT = "0" * 64


def rewrite_function_calls(source: str, name: str) -> str:
    token = name + "("
    out = []
    cursor = 0
    while True:
        start = source.find(token, cursor)
        if start < 0:
            out.append(source[cursor:])
            break
        out.append(source[cursor:start])
        open_pos = start + len(name)
        i = open_pos + 1
        stack = ["("]
        quote = None
        escaped = False
        while i < len(source) and stack:
            ch = source[i]
            if quote is not None:
                if escaped:
                    escaped = False
                elif ch == "\\":
                    escaped = True
                elif ch == quote:
                    quote = None
            else:
                if ch in ('"', "'", '`'):
                    quote = ch
                elif ch in "([{":
                    stack.append(ch)
                elif ch in ")]}":
                    stack.pop()
            i += 1
        if stack:
            raise SystemExit(f"unterminated {name} call")
        inner = source[open_pos + 1 : i - 1]
        args = []
        last = 0
        depths = {"(": 0, "[": 0, "{": 0}
        quote = None
        escaped = False
        pairs = {')': '(', ']': '[', '}': '{'}
        for idx, ch in enumerate(inner):
            if quote is not None:
                if escaped:
                    escaped = False
                elif ch == "\\":
                    escaped = True
                elif ch == quote:
                    quote = None
                continue
            if ch in ('"', "'", '`'):
                quote = ch
            elif ch in depths:
                depths[ch] += 1
            elif ch in pairs:
                depths[pairs[ch]] -= 1
            elif ch == ',' and all(v == 0 for v in depths.values()):
                args.append(inner[last:idx].strip())
                last = idx + 1
        args.append(inner[last:].strip())
        if len(args) == 4:
            replacement = f'{name}("zephyr-devnet-1", {args[0]}, {args[1]}, {args[2]}, "{ZERO_ROOT}", {args[3]})'
        else:
            replacement = source[start:i]
        out.append(replacement)
        cursor = i
    return "".join(out)


envelope = Path("internal/tx/envelope.go")
s = envelope.read_text()
s = s.replace("pad32(", "padP256Int32(")
envelope.write_text(s)

transport = Path("internal/api/transport_identity.go")
s = transport.read_text()
if "func signTransportIdentityPayload(" not in s:
    s += "\nfunc signTransportIdentityPayload(privateKey *ecdsa.PrivateKey, payload string) (string, error) {\n\treturn tx.SignPayload(privateKey, payload)\n}\n"
transport.write_text(s)

for path in Path("internal").rglob("*_test.go"):
    source = path.read_text()
    rewritten = rewrite_function_calls(source, "consensus.BlockHash")
    if rewritten != source:
        path.write_text(rewritten)
