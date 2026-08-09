---
title: Storan dan kapasiti
category: recording
categoryLabel: Rakaman & storan
summary: Berapa banyak kamera yang mampu ditanggung mesin ini, dan bagaimana pengekalan bertukar ganti dengan cakera.
order: 420
---

# Storan dan kapasiti

Dua soalan yang berasingan, dan mencampuradukkannya ialah punca biasa pemasangan yang mengecewakan:

- **Berapa banyak kamera yang boleh diproses mesin ini?** Dihadkan oleh CPU, GPU dan memori.
- **Berapa lama ia boleh menyimpan rakaman?** Dihadkan oleh cakera.

## Anggaran kapasiti {#estimate}

**Tetapan → Kesihatan Mesin** membawa anggaran kapasiti kamera, dan bestari persediaan menunjukkan
angka yang sama.

Ia memodelkan pengesanan AI, memori dan rakaman sebagai beban kerja berterusan dan melaporkan
bilangan kamera yang boleh ditanggung hos berserta **sumber yang mengehadkan** — CPU, GPU, memori atau
cakera. Paparan langsung sengaja tidak dikira: ia atas permintaan, dan tiada sesiapa memerhati setiap
kamera serentak.

Ia hadir dalam tiga gred keyakinan, dan perbezaannya nyata:

| Gred | Datang daripada |
|---|---|
| **Anggaran kasar** | Perkakasan yang dikesan sahaja. |
| **Diukur daripada beban langsung** | Diekstrapolasi daripada kos sebenar kamera yang berjalan. |
| **Ditentukur pada hos ini** | Penanda aras pengesan sebenar pada mesin ini. |

**Jalankan penentukuran** sebelum anda membeli kamera. Ia mengambil kira-kira seminit, paling baik
dijalankan semasa mesin senggang, dan jauh lebih bernilai daripada tekaan helaian spesifikasi —
pemprosesan sebenar pada perkakasan sebenar menyimpang jauh daripada teori.

## Apa yang sebenarnya memakan kapasiti {#drivers}

Mengikut urutan kesan secara kasar:

1. **Resolusi strim pengesanan.** Menghalakan pengesanan ke strim utama 4K dan bukan sub-strim boleh
   memakan CPU beberapa kali ganda tanpa pengesanan tambahan. Inilah perkara pertama yang perlu
   disemak pada mesin yang terlebih beban. Lihat [tab Strim](camera-properties#stream).
2. **Bilangan model aktif.** Setiap model aktif membuat inferens pada setiap bingkai. Dua model
   kira-kira dua kali kerja — nyahaktifkan yang tidak anda gunakan.
3. **Saiz model stok.** Nano hingga sangat besar merentangi julat yang sangat luas. Pada CPU atau
   Raspberry Pi, kekal pada nano atau kecil.
4. **Paparan langsung MJPEG.** Dinding di mana jubin menyatakan "sandaran MJPEG" dan bukan "Langsung"
   memakan jauh lebih banyak bagi setiap kamera. Membetulkan kodek kamera kepada H.264 mengembalikan
   kapasiti itu.
5. **Pengecaman wajah**, pada setiap kamera yang mendayakannya.

Perhatikan bahawa empat daripada lima itu ialah konfigurasi, bukan perkakasan. Mesin yang "tidak
mampu" biasanya mempunyai salah satu daripadanya yang salah.

## Cakera, pengekalan dan kamera {#disk}

Cakera tidak menghadkan berapa banyak kamera yang berjalan — ia menghadkan berapa banyak sejarah yang
anda simpan. Tiga perkara bergandaan:

```
storan  ≈  kamera  ×  kadar bit  ×  hari pengekalan
```

Ubah mana-mana satu dan yang lain bergerak. Jika anggaran menyatakan cakera ialah sumber yang
mengehadkan anda, anda mempunyai empat tuas:

- **Tambah storan.** Penyelesaian yang jujur.
- **Turunkan kadar bit atau resolusi rakaman.** Murah, dan memakan butiran bukti.
- **Pendekkan pengekalan.** Murah, dan memakan sejarah.
- **Rakam lebih sedikit kamera secara berterusan.** Sesetengah kamera memang hanya perlukan klip
  peristiwa.

Anggaran kapasiti melayan rakaman sebagai penimbal berputar dan bukan dinding keras: daripada
mengisytiharkan cakera kecil tidak mampu menjalankan kamera, ia mengehadkan bilangan pada kira-kira
pengekalan minimum sehari dan memberitahu anda pengekalan yang sebenarnya boleh dicapai. Itulah
nombor yang perlu dirundingkan.

## Tetapkan pengekalan daripada masa semakan {#retention-policy}

Kesilapan storan yang paling biasa ialah membeli cakera untuk pengekalan yang tiada sesiapa pilih.

Tanyalah sebaliknya: **berapa lama, di tapak ini, sebelum seseorang sempat menyemak sesuatu
insiden?** Jika itu dua minggu, pengekalan tujuh hari bermakna jawapan kepada "boleh kita lihat?"
kerap tidak, dan setiap ringgit yang dibelanjakan untuk kamera tidak membeli apa-apa bagi peristiwa
tersebut.

Saizkan cakera daripada nombor itu. Jika anda tidak mampu, pendekkan pengekalan secara sengaja dan
beritahu orang yang akan bertanya, bukan menemuinya semasa insiden.

## Kesihatan mesin {#machine-health}

**Tetapan → Kesihatan Mesin** juga memantau CPU, memori dan cakera hos dan menimbulkan pemberitahuan
Kesihatan Mesin apabila ia kekurangan.

Ambil serius amaran cakera — itulah yang mempunyai akibat keras. Rakaman berhenti apabila cakera
berhenti, dan mitigasi disempadankan kepada volum rakaman, dan itulah sebabnya menghalakan laluan
storan ke pemacu sistem ialah idea yang buruk: pemacu sistem yang penuh menjatuhkan keseluruhan
peranti, bukan hanya rakaman.
