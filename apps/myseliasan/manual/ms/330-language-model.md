---
title: Menyediakan model bahasa
category: agent
categoryLabel: Ejen AI
summary: Mati, satu titik akhir dalam rangkaian anda, atau model yang dijalankan sendiri oleh aplikasi ini — termasuk di tapak terasing.
order: 330
---

# Menyediakan model bahasa

Model bahasa ialah pilihan **tidak wajib**. Tanpanya, [ringkasan](fleet-digest) tetap mengira
penemuannya dan manual tetap boleh dicari; yang hilang ialah naratif yang mudah dibaca dan
[pembantu](ask-the-fleet).

Semua yang di bawah berjalan pada rangkaian anda sendiri. Tiada apa-apa dihantar ke mana-mana awan.

## Tiga mod {#modes}

**Off** — ringkasan sahaja, tiada model bahasa. Beginilah satah kawalan dihantar.

**External** — pelayan serasi OpenAI yang sudah ada dalam rangkaian anda. Anda memberikan URL titik
akhir, kunci API (jika ada), dan nama model. Gunakan ini apabila anda sudah menjalankan inferens di
suatu tempat yang perkakasannya lebih besar daripada satah kawalan.

**Sidecar** — proses `llama-server` yang dimulakan, diselia dan dimulakan semula oleh aplikasi ini
untuk anda, mendengar pada loopback sahaja. Gunakan ini apabila satah kawalan ialah satu-satunya
mesin yang ada.

**Test** menduga titik akhir yang baru anda taip *sebelum* anda menyimpannya, supaya kesilapan taip
tertangkap ketika anda masih memandang medan itu dan bukan pada kali berikutnya pembantu ditanya
soalan.

## Memasang sidecar {#install}

Dua artifak: **binari pelayan** dan **fail model** (`.gguf`).

Jika satah kawalan mempunyai akses internet, muat turun kedua-duanya daripada halaman tetapan. Model
lalai bersaiz sekitar satu gigabait; kemajuannya muncul di bawah butang.

## Pemasangan terasing {#air-gap}

Satah kawalan terasing tidak boleh memuat turun apa-apa, dan inilah kes yang ciri ini dibina
untuknya.

**Import** memasang daripada fail yang anda bawa masuk — arkib pelayan, atau mana-mana model `.gguf`
yang sudah anda miliki. Import sengaja tidak dipin: tujuannya ialah menerima artifak yang dibawa
masuk oleh operator dengan pemacu USB. Checksum bagi apa yang anda pasang direkodkan dalam log
pemasangan dan jejak audit, jadi asal usul itu *direkodkan* dan bukan *dikuatkuasakan*.

Muat turun juga boleh dimatikan sepenuhnya untuk satah kawalan, dan dalam keadaan itu halaman ini
menyatakannya serta menawarkan Import sebagai ganti.

## Memilih model {#model-choice}

Model yang lebih besar menjawab dengan lebih baik dan lebih perlahan. Pada CPU, pertukaran itulah
keseluruhan keputusannya.

Model lalai cukup kecil untuk berjalan pada satah kawalan yang sederhana dan memadai untuk naratif
ringkasan serta soalan yang mudah. Model yang lebih besar menulis naratif yang jauh lebih kemas,
memetik dengan lebih boleh dipercayai, dan — inilah perbezaan praktikalnya — **mengekalkan bahasa
bukan Latin dengan betul**. Jika operator anda membaca bahasa Arab atau Cina dan model kecil terus
menjawab dalam bahasa Inggeris, itu masalah model, bukan tetapan.

## Penalaan {#tuning}

| Tetapan | Apa yang ia tentukan |
|---|---|
| **Context size** | Berapa banyak yang boleh diberikan kepada model sekali gus. Ringkasan dan pembantu kedua-duanya berbelanjawan terhadapnya. |
| **CPU threads** | 0 bermaksud auto, yang biasanya betul. |
| **Request timeout** | Berapa lama jawapan yang perlahan dibenarkan mengambil masa. |
| **Max answer length** | Menghadkan balasan, dalam token. |
| **Loopback port** | Sidecar mendengar di sini; ia tidak didedahkan ke luar mesin. |

Inferens pada CPU memang perlahan — token pertama dalam beberapa saat dan naratif ringkasan penuh
dalam puluhan saat adalah biasa, bukan kerosakan.

## Apa yang berlaku apabila model tidak tersedia {#degradation}

Setiap laluan merosot secara terancang dan bukan gagal.

- Sedang bermula (memuatkan model mengambil masa) — pembantu menyatakannya dan meminta anda mencuba
  sebentar lagi.
- Gagal atau tidak dipasang — pembantu tidak tersedia dan menyatakan sebabnya; ringkasan tetap
  berjalan.
- Terhempas — sidecar dimulakan semula secara automatik. **Restart sidecar** memaksanya serta-merta.

Tiada apa-apa yang penting menunggu inferens. Amaran, peraturan, suapan dan penemuan ringkasan
semuanya dikira tanpanya, dan itulah sifat yang membolehkan anda membiarkan model dimatikan.

## Siapa yang boleh mengubah ini {#permissions}

Memasang, mengimport dan menghalakan satah kawalan ke sesuatu titik akhir ialah **superadmin
sahaja**, tanpa mengira matriks kebenaran. Tindakan ini boleh memuat turun gigabait atau menghalakan
satah kawalan ke alamat rangkaian sewenang-wenangnya, jadi ia tidak diwakilkan.

Membaca ringkasan dan menggunakan pembantu pula ialah pemberian biasa yang boleh diberikan kepada
sesuatu peranan.
