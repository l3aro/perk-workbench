#!/bin/bash
pg_restore --no-owner --no-privileges -U "$POSTGRES_USER" -d "$POSTGRES_DB" /init-data/employees.sql.gz
