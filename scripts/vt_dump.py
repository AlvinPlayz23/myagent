import pyte
import sys

path = sys.argv[1] if len(sys.argv) > 1 else '/tmp/pager_capture.txt'
cols = int(sys.argv[2]) if len(sys.argv) > 2 else 80
rows = int(sys.argv[3]) if len(sys.argv) > 3 else 24

data = open(path, 'rb').read().decode('utf-8', errors='replace')
screen = pyte.Screen(cols, rows)
stream = pyte.ByteStream(screen)
stream.feed(data.encode('utf-8'))
for y in range(rows):
    line = screen.display[y].rstrip()
    print(f"{y:2d}|{line}")
