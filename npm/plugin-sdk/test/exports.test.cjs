'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const sdk = require('../index.cjs');

test('exports the public API surface', () => {
  assert.equal(typeof sdk.createPluginServer, 'function');
  assert.equal(typeof sdk.RequestCancelledError, 'function');
});

test('enum constants are frozen with the v1 values', () => {
  assert.deepEqual(sdk.FormFieldKind, { Input: 0, Password: 1, Select: 2 });
  assert.deepEqual(sdk.FormValidation, { None: 0, Required: 1, Port: 2 });
  assert.deepEqual(sdk.IndexKind, { PrimaryKey: 1, Unique: 2, Regular: 3 });
  assert.deepEqual(sdk.RowWriteOperation, { Insert: 'insert', Update: 'update', Delete: 'delete' });
  assert.deepEqual(sdk.DocumentWriteOperation, {
    Read: 'read',
    Insert: 'insert',
    Replace: 'replace',
    Delete: 'delete',
  });
  assert.deepEqual(sdk.ValueKind, {
    Default: 'default',
    Null: 'null',
    String: 'string',
    Bool: 'bool',
    Integer: 'integer',
    Float: 'float',
    Bytes: 'bytes',
    Decimal: 'decimal',
    Timestamp: 'timestamp',
    Array: 'array',
    Object: 'object',
  });
  assert.equal(
    sdk.DocumentFormat.MongoExtendedJSON,
    'application/vnd.perk.mongodb.extjson+json;version=2;mode=relaxed',
  );
  for (const constant of [
    sdk.FormFieldKind,
    sdk.FormValidation,
    sdk.DocumentFormat,
    sdk.IndexKind,
    sdk.ValueKind,
    sdk.RowWriteOperation,
    sdk.DocumentWriteOperation,
  ]) {
    assert.ok(Object.isFrozen(constant));
  }
});

test('RequestCancelledError carries the -32800 code', () => {
  const error = new sdk.RequestCancelledError();
  assert.equal(error.code, -32800);
  assert.equal(error.name, 'RequestCancelledError');
  assert.ok(error instanceof Error);
});
