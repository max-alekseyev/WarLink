const fs = require('fs');
const path = require('path');
const https = require('https');
const http = require('http');

const rootDir = path.resolve(__dirname, '..');
const tablePath = path.join(rootDir, 'ITEMS_TRANSLATION.md');
const dbPath = path.join(rootDir, 'internal/progression/progression_db.json');

if (!fs.existsSync(tablePath)) {
    console.error('File ITEMS_TRANSLATION.md not found.');
    process.exit(1);
}

function downloadImage(url, destPath) {
    return new Promise((resolve, reject) => {
        const client = url.startsWith('https') ? https : http;
        const file = fs.createWriteStream(destPath);
        client.get(url, (response) => {
            if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
                return downloadImage(response.headers.location, destPath).then(resolve).catch(reject);
            }
            if (response.statusCode !== 200) {
                return reject(new Error('Failed download HTTP ' + response.statusCode));
            }
            response.pipe(file);
            file.on('finish', () => {
                file.close(resolve);
            });
        }).on('error', (err) => {
            fs.unlink(destPath, () => {});
            reject(err);
        });
    });
}

async function run() {
    let tableContent = fs.readFileSync(tablePath, 'utf8');
    const db = JSON.parse(fs.readFileSync(dbPath, 'utf8'));

    const lines = tableContent.split('\n');
    let updatedLines = [];
    let updatedTranslationsCount = 0;
    let downloadedImagesCount = 0;

    const translationsMap = new Map(); // id -> ru_name

    for (let line of lines) {
        if (!line.startsWith('|') || line.startsWith('| №') || line.startsWith('|---')) {
            updatedLines.push(line);
            continue;
        }

        const cols = line.split('|').map(c => c.trim());
        if (cols.length < 7) {
            updatedLines.push(line);
            continue;
        }

        // cols: ["", №, Картинка, ID предмета, Текущее название, Название в игре (RU), Категория, Разблокировка/Цена, ""]
        const rowNum = cols[1];
        let imgCol = cols[2];
        const idCol = cols[3].replace(/`/g, '').trim();
        const enNameCol = cols[4];
        const ruNameCol = cols[5];

        // 1. Check if user provided an image URL
        const urlMatch = imgCol.match(/https?:\/\/[^\s"'<>|]+/i);
        if (urlMatch) {
            const imgUrl = urlMatch[0];
            const ext = path.extname(imgUrl.split('?')[0]) || '.webp';
            const safeName = idCol.toLowerCase().replace(/[^a-z0-9_]/g, '_') + ext;
            const destPath = path.join(rootDir, 'guides/icons', safeName);
            const relPath = 'guides/icons/' + safeName;

            console.log(`Downloading image for [${idCol}]: ${imgUrl} -> ${relPath}`);
            try {
                await downloadImage(imgUrl, destPath);
                imgCol = `<img src="${relPath}" width="36" height="36" alt="">`;
                downloadedImagesCount++;
            } catch (err) {
                console.error(`Error downloading ${imgUrl}:`, err.message);
            }
        }

        // 2. Check if user filled in a Russian translation
        if (ruNameCol && ruNameCol.length > 0) {
            translationsMap.set(idCol, ruNameCol);
            updatedTranslationsCount++;
        }

        // Reconstruct line
        cols[2] = imgCol;
        updatedLines.push(cols.join(' | '));
    }

    // Save updated markdown table (with local downloaded images)
    if (downloadedImagesCount > 0) {
        fs.writeFileSync(tablePath, updatedLines.join('\n'), 'utf8');
        console.log(`Updated ITEMS_TRANSLATION.md with ${downloadedImagesCount} downloaded images.`);
    }

    // Apply translations to progression_db.json
    if (translationsMap.size > 0) {
        console.log(`Applying ${translationsMap.size} Russian translations to progression_db.json...`);

        if (Array.isArray(db.unlocks)) {
            for (const u of db.unlocks) {
                if (translationsMap.has(u.unlock_id)) {
                    u.name_ru = translationsMap.get(u.unlock_id);
                }
            }
        }

        if (Array.isArray(db.catalog)) {
            for (const c of db.catalog) {
                if (translationsMap.has(c.id)) {
                    c.name_ru = translationsMap.get(c.id);
                }
            }
        }

        fs.writeFileSync(dbPath, JSON.stringify(db, null, 2), 'utf8');
        console.log(`Saved progression_db.json with ${translationsMap.size} translations.`);
    }

    console.log(`Done! Translations found: ${updatedTranslationsCount}, Images downloaded: ${downloadedImagesCount}`);
}

run().catch(console.error);
