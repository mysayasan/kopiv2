---
title: Pemberitahuan dan log amaran
category: daily-use
categoryLabel: Penggunaan harian
summary: Mengendalikan suapan peristiwa, membaca pengesanan, dan mengakui apa yang telah diuruskan.
order: 130
---

# Pemberitahuan dan log amaran

Suapan Pemberitahuan ialah tempat segala yang ingin diberitahu oleh peranti kepada anda tiba.
Kebanyakan orang menghabiskan lebih banyak masa di sini berbanding di mana-mana lagi dalam produk
ini.

## Kategori {#categories}

Suapan membawa empat jenis peristiwa, dan penapis di bahagian atas memisahkannya:

- **Pengesanan AI** — sesuatu peraturan dicetuskan. Ini membawa gambar petikan dan butiran.
- **Kesihatan Kamera** — kamera menjadi tidak boleh dicapai atau kembali.
- **Kesihatan Mesin** — hos kekurangan CPU, memori atau cakera.
- **Keselamatan Log Masuk** — kuncian log masuk.

Menggabungkannya dalam satu suapan adalah disengajakan: "kamera berhenti melapor" dan "kamera nampak
seseorang" kedua-duanya perkara yang anda mahu tahu, dan memisahkannya ke dua skrin bermakna salah
satunya tidak diperhatikan.

Penapis **Belum dibaca / Semua** ialah paksi yang satu lagi. Belum dibaca ialah paparan kerja.

## Membaca pengesanan {#reading}

Pengesanan AI berkembang menjadi gambar petikan pada detik ia dicetuskan, dengan objek yang dikesan
berkotak dan berlabel, berserta peraturan yang dicetuskan, keyakinan, kamera dan cap masa.

**Lihat klip** muncul apabila rakaman yang sepadan wujud. Apabila ia tiada, entri itu menyatakan
**Tiada klip dirakam** — dan itu bermakna salah satu daripada dua perkara:

- Rakaman dimatikan untuk kamera itu ketika peristiwa berlaku, atau
- rakaman itu telah melepasi tempoh pengekalannya dan telah dibersihkan.

Kedua-duanya bukan ralat. Jika anda perlukan klip bagi sesuatu kamera, hidupkan rakaman untuknya;
jika peristiwa lazimnya lebih tua daripada pengekalan anda, naikkan pengekalan itu. Lihat
[Konfigurasi rakaman](recording-configuration).

## Mengakui {#acknowledge}

Mengakui menandakan peristiwa sebagai telah diuruskan dan mengeluarkannya daripada kiraan belum
dibaca. Operator dan pentadbir boleh mengakui; pemerhati tidak boleh.

Anggapnya sebagai baris gilir kerja dan bukan formaliti. Nilai pengakuan ialah kiraan belum dibaca
menjadi ukuran sebenar kerja yang tertunggak — dan panel
[kamera paling bising](dashboard#noise) pada papan pemuka menjadi ukuran sebenar peraturan mana yang
membazirkan perhatian orang.

## Peristiwa diagnostik {#diagnostic}

Sesetengah entri ditanda **diagnostik**. Ini ialah sampel yang direkodkan sistem untuk menunjukkan
kepada anda apa yang dilihat pengesan — berguna semasa menala peraturan, bising selepas itu.

Ia boleh dibersihkan berasingan daripada pengesanan sebenar, jadi membersihkan kekusutan penalaan
tidak sekali-kali menyentuh sejarah pengesanan sebenar anda.

## Log amaran {#alert-log}

Suapan ialah paparan langsung terkini. **Log Amaran** pada halaman Pengesanan AI sesuatu kamera ialah
sejarah penuh yang boleh dicari bagi kamera itu: tapis mengikut masa, peristiwa, keyakinan atau
keadaan, isih mana-mana lajur, dan halamankannya.

Log itu menanya pangkalan data secara terus dan bukan menapis apa yang sudah ada pada skrin, jadi ia
kekal berguna pada kamera dengan sejarah panjang — iaitu tepat kamera yang akan anda cari.

## Apabila amaran tidak sampai {#not-arriving}

Turuti senarai ini mengikut urutan:

1. **Adakah ada peraturan?** Rakaman tidak menghasilkan amaran. Peraturan pengesanan yang
   menghasilkannya. Semak halaman Pengesanan AI kamera itu.
2. **Adakah peraturan itu didayakan, dan adakah ia dalam jadual?** Peraturan berjadual tidak aktif di
   luar jadualnya.
3. **Adakah kamera dalam talian?** Semak titik pada rel, atau [Kesihatan kamera](camera-health).
4. **Adakah runtime AI sedia?** Tetapan → AI. Tanpa model, tiada apa yang dikesan.
5. **Adakah ia tiba dalam suapan tetapi tidak pada telefon anda?** Maka pengesanan berjalan baik dan
   penghantaran tidak — lihat [Destinasi pemberitahuan](notification-destinations).

Urutan itu penting: setiap langkah lebih murah untuk disemak berbanding langkah selepasnya, dan
langkah 5 ialah tempat kebanyakan orang bermula dan paling banyak membazir masa.
