const fs = require('fs');
const path = require('path');
const assert = require('assert');

// Read the actual progression.js file
const progressionCode = fs.readFileSync(path.join(__dirname, '../ui/js/progression.js'), 'utf8');

// Mock window and document
const window = {
    UIStore: {
        remove: (key) => { window._removedKey = key; },
        set: () => {},
        requestSWR: () => {}
    }
};
global.window = window;
global.UIStore = window.UIStore;

// Extract render function specifically inside loadCatalogDatabase
const catDbIdx = progressionCode.indexOf('async function loadCatalogDatabase');
const catDbCode = progressionCode.slice(catDbIdx);
const renderMatch = catDbCode.match(/const render = \(data\) => \{([\s\S]*?)\n        \};/);
if (!renderMatch) {
    throw new Error('Could not find render function in loadCatalogDatabase');
}

// Build test runner from actual progression.js render code
const testFn = new Function('data', 'force', `
    let allUnlocks = [];
    let reloadTriggered = false;
    let lastRenderedCatalogKey = '';
    function updateCareerNextReward() {}
    function updateTargetBanner() {}
    function renderCatalog() {}
    function loadCatalogDatabase(f) {
        if (f) reloadTriggered = true;
    }

    const render = (data) => {
        ${renderMatch[1]}
    };

    render(data);

    return { allUnlocks, reloadTriggered };
`);

// Test 1: New payload with version 3.0.0 and 411 unlocks
const payloadV3 = {
    version: '3.0.0',
    total_unlocks: 411,
    unlocks: new Array(411).fill({ unlock_id: 'item', name: 'Item' })
};
const res1 = testFn(payloadV3, false);
assert.strictEqual(res1.allUnlocks.length, 411, `Expected 411 unlocks, got ${res1.allUnlocks.length}`);
assert.strictEqual(res1.reloadTriggered, false, 'Should not reload for valid v3.0.0 payload');
console.log('PASS Test 1: Valid v3.0.0 payload populates 411 unlocks');

// Test 2: Payload without explicit version (legacy fallback)
const payloadNoVer = {
    unlocks: [ { unlock_id: 'test' } ]
};
const res2 = testFn(payloadNoVer, false);
assert.strictEqual(res2.allUnlocks.length, 1, `Expected 1 unlock for legacy payload, got ${res2.allUnlocks.length}`);
assert.strictEqual(res2.reloadTriggered, false, 'Should not reload for legacy payload without metadata');
console.log('PASS Test 2: Legacy payload without version correctly populates unlocks without discarding');

// Test 3: Outdated payload (e.g. cached Season 2 data)
const payloadOld = {
    version: '2.2.0',
    total_unlocks: 264,
    unlocks: new Array(264).fill({ unlock_id: 'old' })
};
const res3 = testFn(payloadOld, false);
assert.strictEqual(res3.reloadTriggered, true, 'Must trigger reload for outdated cache');
console.log('PASS Test 3: Outdated cache correctly triggers reload');
