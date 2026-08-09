---
title: Menguruskan kamera melalui ONVIF
category: cameras
categoryLabel: Kamera
summary: Baca identiti kamera, tetapkan jam dan rangkaiannya, urus akaunnya, but semula atau set semula.
order: 240
---

# Menguruskan kamera melalui ONVIF

Tab **ONVIF** sesuatu kamera menguruskan kamera itu sendiri, bukan cara peranti ini menggunakannya.
Ia menjimatkan perjalanan ke antara muka web kamera untuk perkara yang anda benar-benar lakukan.

Semua yang di sini memerlukan kelayakan kamera yang berfungsi dan kamera yang menyokong operasi
berkenaan. Tab menunjukkan apa yang diiklankan setiap kamera, jadi kawalan yang tiada bermakna
perisian tegar tidak menawarkannya.

## Identiti {#identity}

Pengeluar, model, versi perisian tegar, pengecam perkakasan dan siri, alamat MAC, versi ONVIF, dan
perkhidmatan yang diiklankan kamera.

Dua kegunaan praktikal. **Versi perisian tegar** ialah perkara pertama yang perlu disemak apabila
satu kamera berkelakuan berbeza daripada kamera yang serupa di sebelahnya. **Alamat MAC** ialah cara
anda mencari kamera dalam suis atau pelayan DHCP anda apabila IPnya telah berpindah.

## Jam {#clock}

Membaca masa semasa kamera dan membolehkan anda menetapkan sumbernya.

- **NTP (automatik)** — kamera menyegerak daripada pelayan masa. Gunakan ini.
- **Manual** — menetapkan jam kamera kepada masa semasa peranti ini apabila disimpan.

Turut kelihatan: zon waktu, pengendalian jimat cahaya siang, dan sama ada pelayan NTP datang
daripada DHCP atau ditetapkan secara eksplisit.

Ini lebih penting daripada rupanya. **Kamera dengan jam yang salah menghasilkan rakaman dengan cap
masa yang salah**, dan apabila anda mengaitkan rakaman dengan log pintu atau kenyataan saksi, kamera
yang tersasar sebelas minit lebih teruk daripada tiada kamera. Jika anda membetulkan satu perkara
pada tab ini, betulkan yang ini — dan betulkan pada setiap kamera, bukan hanya yang anda sedang
lihat.

## Rangkaian {#network}

Membaca konfigurasi IP kamera dan boleh mengubahnya: DHCP hidup atau mati, alamat IPv4, panjang
awalan, get laluan dan pelayan DNS.

> [!WARNING]
> Alamat, awalan atau get laluan yang salah menjadikan kamera tidak boleh dicapai, dan satu-satunya
> jalan kembali biasanya butang set semula fizikal. Peranti meminta anda mengesahkan atas sebab ini.

Jika anda mahukan kamera pada alamat tetap, tempahan DHCP pada pelayan anda lebih selamat daripada
alamat statik pada kamera. Ia bertahan selepas set semula kilang kamera dan ia ditukar dari tempat
yang masih boleh anda capai.

Jika anda memang menetapkan alamat statik, ubahnya daripada mesin pada subnet yang sama, dan pastikan
kamera menjawab pada alamat baharu sebelum anda menutup halaman.

## Pengguna kamera {#users}

Akaun ONVIF kamera itu sendiri: senaraikan, tambah, tukar kata laluan, buang. Peranan ialah peranan
kamera — Administrator, Operator, User.

Amalan yang berguna ialah memberi peranti ini akaun khusus bukan pentadbir dengan hak yang mencukupi
untuk menstrim dan melakukan pengurusan yang anda perlukan, dan mengekalkan log masuk admin kamera
untuk manusia. Apabila kata laluan perakam perlu berubah, tepat satu perkara berubah, dan log kamera
itu sendiri memberitahu anda capaian mana yang datang daripada perakam.

## Penyelenggaraan {#maintenance}

**But semula** memulakan semula kamera. Ia langkah pertama yang betul bagi kamera yang menjawab
tetapi menstrim dengan buruk, dan ia memakan seminit rakaman.

**Set semula lembut** memulihkan kamera kepada lalai tetapi mengekalkan tetapan rangkaiannya. Kamera
kekal boleh dicapai; profil, kelayakan dan tetapan imejnya tidak bertahan.

**Set semula keras** memulihkan lalai kilang penuh **termasuk rangkaian**. Kamera besar kemungkinan
akan kembali pada alamat berbeza — mungkin DHCP sedangkan anda ada statik — dan anda mungkin terpaksa
menemuinya semula. Jangan lakukan ini dari jauh pada kamera yang tidak boleh anda capai secara
fizikal.

Selepas sebarang set semula, semak semula [tab Strim](camera-properties#stream) kamera: profil
biasanya dinomborkan semula, dan tugasan yang digunakan peranti ini tidak lagi menghala ke tempat
yang anda sangka.

## Apabila kawalan ONVIF tiada atau gagal {#limits}

- **Kawalan tidak ditunjukkan.** Kamera tidak mengiklankan perkhidmatan itu. Gunakan antara muka
  webnya sendiri.
- **Kawalan ditunjukkan tetapi gagal.** Biasanya akaun itu kekurangan hak — akaun operator ONVIF
  selalunya tidak boleh menukar tetapan rangkaian. Cuba semula dengan akaun pentadbir pada kamera.
- **Segala pada tab itu gagal.** Syaki kelayakan tersimpan dahulu; lihat
  [Kesihatan kamera](camera-health#troubleshooting).
