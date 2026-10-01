#!/usr/bin/env python3
"""Generate the profiling inputs, deterministically (random.Random(seed)).

records-<N>.json : one JSON array of N API-record objects, compact
                   (separators ",", ":"), no whitespace. Each record has
                   eight members, in this order:
                     id       integer (the index)
                     name     string  (first + " " + last, from fixed lists)
                     email    string
                     score    float   (round(uniform(0, 1000), 2))
                     active   boolean
                     manager  null
                     address  object {"city": string, "zip": string (5 digits),
                              "floor": integer 0..40}
                     tags     array of 1..3 short strings
flat-<N>.json    : one JSON array of the integers 0..N-1, compact.

Usage: gen.py [OUTDIR]  (default: this directory; the inputs are gitignored)
Writes records-10000.json (~2 MB), records-126000.json (~25 MB) and
flat-300000.json (~1.9 MB).
"""
import json
import os
import random
import sys

FIRST = ["Ada", "Grace", "Alan", "Edsger", "Barbara", "Donald", "Ken", "Dennis",
         "Margaret", "John", "Frances", "Leslie", "Niklaus", "Radia", "Tony"]
LAST = ["Lovelace", "Hopper", "Turing", "Dijkstra", "Liskov", "Knuth", "Thompson",
        "Ritchie", "Hamilton", "McCarthy", "Allen", "Lamport", "Wirth", "Perlman",
        "Hoare"]
CITIES = ["Dublin", "Cork", "Galway", "Limerick", "Waterford", "Kilkenny", "Sligo",
          "Athlone", "Wexford", "Drogheda"]
TAGS = ["admin", "beta", "billing", "eu", "us", "trial", "vip", "legacy", "api",
        "mobile"]


def record(i, rnd):
    first, last = rnd.choice(FIRST), rnd.choice(LAST)
    return {
        "id": i,
        "name": f"{first} {last}",
        "email": f"{first.lower()}.{last.lower()}{i}@example.com",
        "score": round(rnd.uniform(0, 1000), 2),
        "active": rnd.random() < 0.7,
        "manager": None,
        "address": {
            "city": rnd.choice(CITIES),
            "zip": f"{rnd.randrange(100000):05d}",
            "floor": rnd.randrange(41),
        },
        "tags": rnd.sample(TAGS, rnd.randint(1, 3)),
    }


def write_records(path, n, seed):
    rnd = random.Random(seed)
    with open(path, "w") as out:
        out.write("[")
        for i in range(n):
            if i:
                out.write(",")
            out.write(json.dumps(record(i, rnd), separators=(",", ":")))
        out.write("]")


def write_flat(path, n):
    with open(path, "w") as out:
        out.write("[" + ",".join(str(i) for i in range(n)) + "]")


def main():
    outdir = sys.argv[1] if len(sys.argv) > 1 else os.path.dirname(os.path.abspath(__file__))
    write_records(os.path.join(outdir, "records-10000.json"), 10000, 20260929)
    write_records(os.path.join(outdir, "records-126000.json"), 126000, 20260929)
    write_flat(os.path.join(outdir, "flat-300000.json"), 300000)


if __name__ == "__main__":
    main()
