# Synthetic protocol fixtures

Every byte in snapshot.bin and every payload/frame in frames.json was synthesized
for public tests. They preserve observed framing and record dimensions, not a
household's data. Pattern names are Example 01 through Example 18; unknown fields
are zeroed or filled with deterministic low-valued bytes. No device identity,
household scene parameters or original captured timestamp is distributed here.

Frame checksums and XOR encodings were computed independently with Python's
hashlib during sanitization; Python is not required to run tests or the app.
These fixtures test parsing, checksums, bounds and simulated recovery. They are
not evidence that a particular controller accepts a write. Original hardware
captures and pre-redaction fixtures remain in ignored private storage.
