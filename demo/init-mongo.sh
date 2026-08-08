#!/bin/sh
# Seeds the MongoDB demo databases on every container start, idempotently.
# A collection is re-imported (with --drop) only when its document count
# differs from the expected count, so existing volumes, stale volumes, and
# partial imports all heal themselves. Expected counts track the JSON files
# in this directory.
set -e

seed() {
  db=$1 coll=$2 file=$3 expected=$4
  count=$(mongosh --quiet mongodb://mongo:27017 --eval "print(db.getSiblingDB('$db').$coll.estimatedDocumentCount())" 2>/dev/null || true)
  if [ "$count" = "$expected" ]; then
    echo "ok  $db.$coll ($count docs)"
  else
    echo "seed $db.$coll ($count -> $expected docs) from /demo/$file"
    mongoimport --host mongo:27017 --db "$db" --collection "$coll" --file "/demo/$file" --drop
  fi
}

# restaurants: MongoDB primer dataset (legacy demo)
seed restaurants restaurants restaurants.json 25359

# atlas: MongoDB Atlas sample datasets — heterogeneous schemas, inconsistent
# columns, and the full BSON type spread (dates, doubles, ints, longs,
# Decimal128, ObjectId, arrays, nested docs, GeoJSON, nulls, missing fields)
seed atlas movies atlas-movies.json 4707
seed atlas theaters atlas-theaters.json 1564
seed atlas users atlas-users.json 185
seed atlas sales atlas-sales.json 5000
seed atlas shipwrecks atlas-shipwrecks.json 11095
seed atlas accounts atlas-accounts.json 1746
seed atlas customers atlas-customers.json 500
