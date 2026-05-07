# Assumes plugin repos checked out at same level

cd ../path
npm link amagama
npm run build
npm test

cd ../directive
npm link amagama
npm run build
npm test

cd ../multisource
npm link amagama
npm run build
npm test

cd ../hoover
npm link amagama
npm run build
npm test

cd ../expr
npm link amagama
npm run build
npm test

cd ../csv
npm link amagama
npm run build
npm test

cd ../toml
npm link amagama
npm run build
npm test

cd ../ini
npm link amagama
npm run build
npm test

cd ../amagama

