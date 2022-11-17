import sys
import base64

b64 = base64.b64decode(sys.argv[1])
b64_str = [str(int(c)) for c in b64]

string = ", ".join(b64_str)
print(f"[]byte{{{string}}}")

