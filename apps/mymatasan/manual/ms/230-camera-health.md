---
title: Kesihatan kamera dan kamera luar talian
category: cameras
categoryLabel: Kamera
summary: Cara kebolehcapaian dipantau, dan cara menangani kamera yang telah menjadi luar talian.
order: 230
---

# Kesihatan kamera dan kamera luar talian

## Cara kesihatan dipantau {#monitoring}

Peranti menduga setiap kamera mengikut jadual dan merekodkan sama ada ia menjawab. Hasilnya ialah
titik berwarna di sebelah setiap kamera pada rel, status pada halamannya, dan panel
[Kebolehpercayaan kamera](dashboard#reliability) pada papan pemuka.

Menjadi luar talian menimbulkan pemberitahuan **Kesihatan Kamera**, dan kembali menimbulkan satu
lagi. Kedua-duanya penting: kamera yang terputus dan pulih setiap malam pada waktu yang sama sedang
memberitahu anda sesuatu yang pandangan sekali seminggu tidak akan sekali-kali menangkapnya.

Selang tinjauan, berapa banyak kegagalan berturut-turut dikira luar talian, dan sama ada luar talian
menimbulkan amaran, boleh dikonfigurasikan dalam **Tetapan → Kesihatan Kamera**. Menjadikan peranti
lebih sabar mengurangkan bunyi pemberitahuan pada rangkaian yang tidak stabil — dengan kos
mengetahui lebih lewat bahawa kamera itu benar-benar hilang.

## Apa sebenarnya maksud luar talian {#meaning}

Luar talian bermakna *peranti ini tidak dapat mencapai kamera ini*. Ia tidak bermakna kamera itu
rosak, dan ia tidak selalunya bermakna ia berhenti merakam.

Tiga keadaan berbeza menghasilkan titik yang sama:

- Kamera dimatikan, ranap atau dicabut.
- Kamera baik-baik sahaja tetapi laluan rangkaian antara ia dan peranti tidak.
- Kamera baik dan boleh dicapai tetapi menolak kelayakan.

Yang ketiga wajar diasingkan awal, kerana ia kelihatan sama daripada rel dan mempunyai penyelesaian
yang sama sekali berbeza.

## Menangani kamera luar talian {#troubleshooting}

Mengikut urutan — setiap langkah lebih murah daripada langkah seterusnya.

**1. Adakah hanya yang ini?** Jika setiap kamera pada satu suis menjadi luar talian bersama-sama,
berhenti melihat kamera.

**2. Bolehkah peranti mencapainya langsung?** Daripada rangkaian peranti itu sendiri, buka antara
muka web kamera. Jika itu gagal, ia masalah rangkaian atau kuasa dan tiada apa dalam produk ini yang
akan membetulkannya.

**3. Adakah ia menjawab, tetapi menolak?** Jika antara muka kamera berfungsi tetapi peranti masih
menyatakan luar talian, syaki kelayakan. Seseorang menukar kata laluan kamera ialah punca paling
biasa bagi kamera sihat yang dibaca sebagai luar talian. Betulkannya pada
[tab Akses](camera-properties#access) kamera.

**4. Adakah alamatnya berubah?** Kamera pada DHCP boleh mendapat alamat baharu selepas gangguan
kuasa, dan peranti masih memanggil yang lama. Sama ada berikan tempahan pada pelayan DHCP anda, atau
tetapkan alamat statik pada [tab ONVIF](onvif-management#network) kamera.

**5. Adakah profil strimnya masih sah?** Kemas kini perisian tegar kadangkala menomborkan semula atau
membuang profil. **Cari Strim** pada [tab Strim](camera-properties#stream) membaca semula apa yang
sebenarnya ditawarkan kamera sekarang.

**6. Duga semula.** Senarai kamera boleh disemak semula atas permintaan dan bukan menunggu tinjauan
berjadual seterusnya.

## Kamera yang tidak stabil {#flapping}

Kamera yang berselang-seli antara dalam talian dan luar talian lebih memudaratkan daripada kamera
yang mati bersih, kerana ia menghasilkan aliran pemberitahuan yang orang belajar mengabaikannya, dan
rakamannya penuh jurang yang tiada sesiapa perasan.

Punca biasa, mengikut urutan: pautan tanpa wayar, bajet PoE yang melebihi had pada suis, kabel yang
marginal, dan kamera yang CPUnya tepu dengan terlalu banyak strim serentak — yang boleh jadi adalah
anda, jika paparan langsung, pengesanan dan rakaman semuanya menarik profil beresolusi tinggi yang
berasingan. Menyatukan kepada sub-strim untuk paparan langsung dan pengesanan membetulkan lebih
banyak "kamera tidak stabil" berbanding menggantikan perkakasan.

Semak **bilangan gangguan** dan bukan peratusan masa beroperasi pada
[Kebolehpercayaan kamera](dashboard#reliability); empat puluh gangguan pendek dan satu gangguan
panjang memberi peratusan yang sama dan bermaksud perkara yang sama sekali berbeza.

## Apa yang berlaku kepada pengesanan dan rakaman {#consequences}

Semasa kamera luar talian, tiada apa untuk dirakam dan tiada apa untuk dikesan. Garis masanya
menunjukkan jurang, dan tiada peraturan padanya boleh dicetuskan.

Inilah sebabnya amaran kesihatan kamera berhak mendapat perhatian yang sama seperti amaran
pengesanan. Kamera yang tiada sesiapa perasan telah luar talian selama seminggu ialah seminggu tanpa
rakaman daripadanya — dan amaran yang sepatutnya memberitahu anda tidak pernah dicetuskan, kerana
tiada apa untuk dicetuskan.
