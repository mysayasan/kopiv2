---
title: Laporan
category: reports
categoryLabel: Laporan
summary: Empat PDF boleh cetak, dijana pada satah kawalan sendiri — dan satu batasan yang perlu diketahui.
order: 410
---

# Laporan

**Reports** menghasilkan PDF armada anda yang boleh dicetak atas permintaan: pratonton satu pada
skrin, kemudian cetak atau muat turun.

Ia dijana **pada satah kawalan itu sendiri**. Tiada perkhidmatan luar, tiada pelayar tanpa kepala,
tiada fon dimuat turun semasa penjanaan — dan itulah yang membolehkan tapak terasing menghasilkan
laporan sama sekali.

## Empat laporan {#reports}

**Fleet Health** — status dalam talian/luar talian setiap nod, senarai tarikh luput sijil, dan
ringkasan amaran sepanjang tempoh itu. Inilah yang sesuai dilampirkan pada semakan operasi bulanan,
dan senarai sijil ialah bahagian yang orang bersyukur telah melihatnya lebih awal.

**Site & Asset Inventory** — daftar aset mengikut bangunan, lengkap dengan pelan lantai yang
dijana, penempatan kamera dan peranti di tapak. Nilainya sama tepat dengan ketepatan
[pelan lantai](buildings-and-floors) anda; ia cetakan tentang apa yang anda lukis.

**Incident Detail** — amaran terkini sepanjang tempoh itu berserta butiran setiap peristiwa dan
petikan skrin disertakan sekali. Inilah yang anda serahkan kepada seseorang yang menyiasat satu
malam tertentu.

**Security & Access** — pengguna, peranan, matriks kebenaran titik akhir, jejak audit, dan
perakuan perlindungan data. **Superadmin sahaja**, kerana ia gambaran lengkap tentang siapa boleh
buat apa.

## Tempoh dan tapak {#scope}

Setiap laporan mengambil satu **tempoh**, dan boleh dihadkan kepada satu **tapak**.

Melaporkan mengikut tapak ialah kes biasa bagi armada berbilang lokasi: orang yang menguruskan satu
gudang mahukan gudang itu, dan PDF seluruh armada ialah dokumen yang tiada siapa membacanya sampai
habis.

## Pratonton, cetak, muat turun {#output}

**Preview** menjananya pada skrin supaya anda boleh menyemak skopnya sebelum ia dicetak ke kertas.

**Print** membuka dialog cetak pelayar; jika pelayar menyekat tetingkap timbul, halaman ini
menyatakannya dan **Download** memberikan anda PDF yang sama sebagai fail. Download juga pilihan
yang betul apabila laporan itu akan dihantar melalui e-mel atau dimasukkan ke dalam fail rekod.

## Satu batasan yang perlu diketahui {#latin-only}

Teks laporan dilukis dengan fon tulisan Latin terbina dalam, yang meliputi teks **Inggeris, Melayu
dan Eropah bertanda**.

**Aksara CJK dan Arab dalam nama yang dimasukkan pengguna akan dijana kosong.** Bangunan bernama
仓库 A atau nod bernama بوابة akan menghasilkan ruang kosong dalam PDF di tempat namanya sepatutnya
berada.

Ini batasan v1 yang diketahui dan disengajakan, bukan kegagalan senyap, dan ada jalan praktikal
mengelilinginya: jika laporan anda ditujukan kepada pembaca dalam bahasa tersebut, berikan bangunan,
kawasan dan nod anda nama bertulisan Latin — atau kekalkan nama Latin di samping nama tempatan — dan
laporan kekal boleh dibaca. Selebihnya produk ini diterjemahkan sepenuhnya; hanya PDF sahaja yang
terkekang, dan hanya sehingga fon Unicode disertakan.

## Kebenaran {#permissions}

Menjana laporan ialah keupayaan yang diberikan seperti yang lain, dan **Security & Access ialah
superadmin sahaja** tanpa mengira apa lagi yang telah diberikan kepada sesuatu peranan. Jika sesuatu
laporan ditolak, itu matriks sedang melakukan tugasnya — lihat halaman peranan dan bukan menganggap
ada kerosakan.
