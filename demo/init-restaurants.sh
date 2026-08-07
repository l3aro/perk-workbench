#!/bin/sh
# Imports the MongoDB primer restaurants dataset (demo/restaurants.json) into
# the restaurants database on first container start.
set -e
mongoimport --db restaurants --collection restaurants --file /demo/restaurants.json --drop
