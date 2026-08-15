---
title: Glosari
category: reference
categoryLabel: Rujukan
summary: Perkataan yang digunakan produk ini, dan maksudnya di sini secara khusus.
order: 930
---

# Glosari

**Ketiadaan (absence)** — syarat dalam [peraturan armada](fleet-rules#absence) yang menghendaki
sesuatu **tidak** berlaku. Itulah cara peraturan menyatakan tiada salah: peristiwa yang padan akan
melucutkan peraturan dan bukan menyalakannya.

**Pengambilan (adoption)** — membawa sesebuah nod ke bawah pengurusan satah kawalan ini, menggunakan
kunci armada dan kod tuntutan. Lihat [Mengambil nod](adopting-nodes).

**Log audit** — rekod tambah-sahaja bagi tindakan sensitif, termasuk yang ditolak. Lihat
[Log audit](audit-log).

**Peta asas (basemap)** — jalan dan rupa bumi yang dilukis di bawah peta, disajikan daripada fail
PMTiles tempatan. Bebas daripada data anda sendiri; tanpanya peta tetap memaparkan nod dan bangunan.

**Serah-tangan bootstrap** — menamatkan superadmin lalai setelah akaun superadmin sebenar berfungsi.

**Kod tuntutan** — kod bertempoh pendek yang dijana **pada nod**, membuktikan nod itu bersetuju
untuk diambil sekarang. Kunci armada menyatakan armada yang mana; kod tuntutan menyatakan *dan saya
setuju*.

**Saluran kawalan** — sambungan yang didail **keluar** oleh nod kepada satah kawalan ini, digunakan
untuk pengurusan dan untuk menstrim klip. Itulah sebabnya nod di belakang NAT tidak memerlukan
pemajuan port masuk.

**Satah kawalan** — aplikasi ini: benda yang menguruskan banyak peranti dari satu skrin. Ia tidak
merakam video.

**Tempoh sejuk (cooldown)** — berapa lama peraturan armada berdiam selepas menyala, supaya satu
insiden menjadi satu amaran.

**Ringkasan (digest)** — catatan berkala tentang apa yang dilakukan armada, dibina daripada penemuan
yang dikira dan secara pilihan dinaratifkan oleh model bahasa. Lihat
[Ringkasan armada](fleet-digest).

**Penemuan (finding)** — satu pemerhatian yang dikira di dalam ringkasan (nod yang senyap, lonjakan,
sijil yang akan luput). Dihasilkan oleh kod biasa, bukan oleh model.

**Kunci armada** — rahsia dikongsi yang menjadikan nod boleh ditemui oleh satah kawalan ini dan
tiada yang lain. Layan ia seperti kata laluan.

**Peraturan armada** — peraturan yang mengaitkan peristiwa merentas nod yang **berlainan**. Lihat
[Peraturan armada](fleet-rules).

**Tempoh ihsan (grace delay)** — berapa lama peraturan menunggu sebelum mempercayai sesuatu
ketiadaan, supaya pembaca lencana yang melapor sedikit lewat daripada sesentuh pintu tidak
menghasilkan pencerobohan palsu.

**Nod** — satu peranti terurus: perakam kamera, hab penderia, pengawal pintu.

**Akses nod** — kebenaran untuk memandu nod **itu sendiri** melalui terowong, pada tahap viewer,
operator atau admin. Berasingan daripada matriks kebenaran satah kawalan.

**Menunggu (pending)** — akaun yang disahkan tetapi belum ada peranan. Ia melihat skrin akses
menunggu sehingga pentadbir melepaskannya.

**Matriks kebenaran** — peraturan setiap peranan ke atas awalan laluan API dan kata kerja. Awalan
padanan terpanjang menang; tiada peraturan bermakna ditolak. Ia mentadbir API **dan** menu. Lihat
[Pengguna, peranan dan akses](users-and-roles#access).

**Penempatan (placement)** — kamera yang disemat pada satu titik pada pelan lantai. Setiap kamera
mempunyai tepat satu.

**PMTiles** — format jubin peta fail tunggal yang digunakan untuk peta asas luar talian.

**Melepaskan (release)** — mengeluarkan nod daripada satah kawalan ini supaya ia boleh diambil
semula. Bandingkan dengan **wipe**, yang memadam nod itu.

**Gugur sendiri (self-drop)** — nod yang menyahpasangkan dirinya dari sebelahnya sendiri, untuk
keadaan apabila satah kawalan sudah tiada atau tidak dapat dihubungi. Meninggalkan baris lapuk untuk
dibersihkan di sini.

**Sidecar** — pelayan inferens tempatan yang dimulakan dan diselia oleh aplikasi ini apabila model
bahasa berjalan pada satah kawalan itu sendiri. Lihat
[Menyediakan model bahasa](language-model#modes).

**Tapak (site)** — lokasi yang mengumpulkan segala yang berada di situ. Peta dan laporan boleh
diskopkan kepada satu tapak.

**Superadmin** — peranan yang memintas setiap pemeriksaan kebenaran. Pemasangan model dan penyunting
tetapan adalah superadmin sahaja tanpa mengira matriks.

**Nod tidak dikenali** — nod yang memegang sijil sah tetapi tiada rekodnya pada satah kawalan ini.
Lihat [Mengurus nod](managing-nodes#unrecognized).

**Tetingkap (window)** — sedekat mana peristiwa yang diperlukan oleh peraturan armada mesti berlaku
untuk dikira sebagai satu insiden.

**Wipe** — set semula kilang jarak jauh bagi sesebuah nod: rakaman, kamera, peraturan, pengguna dan
tetapan dipadam, kemudian mula semula. Ia tidak boleh dibatalkan.
