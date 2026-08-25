---
title: Ambil alih
category: fleet
categoryLabel: Armada
summary: Namakan perakam simpanan untuk sesebuah tapak, buktikan ia benar-benar dapat mencapai kamera, dan serahkan kamera itu apabila perakam berhenti.
order: 160
---

# Ambil alih

Perakam ialah satu-satunya yang merakam kameranya sendiri. Apabila ia berhenti — bekalan kuasa
mati, cakera rosak, port suis gagal, atau seseorang membawanya keluar dari bangunan — kamera
itu berhenti dirakam, dan tiada apa-apa di mana-mana yang mula merakamnya semula.

**Pelan ambil alih** menamakan sebuah peranti simpanan bagi sesebuah perakam. Semasa perakam
itu sihat, peranti simpanan diberi salinan senarai kameranya, supaya apabila perakam itu
berhenti, kamera yang diawasinya dirakam semula dalam beberapa minit.

## Satu perkara yang perlu difahami sebelum anda bergantung padanya {#tested}

Penyalinan bukan kesediaan.

Penyalinan membuktikan kedua-dua peranti boleh berhubung antara satu sama lain. Ia tidak
mengatakan apa-apa tentang sama ada peranti simpanan dapat mencapai **kamera** — laluan
rangkaian yang berbeza, kelayakan yang berbeza, dan itulah yang sebenarnya gagal. Peranti
simpanan pada VLAN yang salah, kamera yang kata laluannya telah ditukar pada kamera itu
sendiri, suis yang tidak pernah mempunyai laluan ke tapak berkenaan: tiada satu pun daripadanya
kelihatan dalam satu salinan, dan kesemuanya muncul pada saat seseorang memerlukan rakaman.

Justeru pelan yang telah disalin tetapi belum pernah diuji tertulis **Belum pernah diuji**, dan
skrin tidak melembutkannya. Tekan **Uji** dan peranti simpanan akan cuba membuka setiap satu
kamera tersebut, tepat seperti cara perakaman akan lakukan, lalu melaporkan bagi setiap kamera
sama ada ia berjaya. Barulah pelan itu tertulis **Diuji dan sedia**.

Ujian yang kembali dengan tiga daripada empat puluh kamera dapat dicapai bukanlah kegagalan
ciri ini. Itulah cirinya: anda kini tahu, pada petang yang tenang, sesuatu yang jika tidak akan
anda pelajari semasa insiden berlaku.

Pelan juga diuji semula dengan sendirinya sekali sehari, kerana perkara yang merosakkan sebuah
peranti simpanan — perubahan VLAN, kata laluan yang ditukar, mesin yang dipindahkan ke suis
lain — berlaku ketika tiada apa-apa sedang berlaku.

## Apa yang ia tidak lakukan {#limits}

**Ia memulihkan perakaman, bukan rakaman.** Rakaman yang sudah berada pada peranti yang gagal
masih hanya ada pada peranti itu. Satu-satunya salinan di tempat lain ialah klip yang telah pun
ditarik oleh arkib klip kritikal. Ambil alih bermaksud kamera dirakam semula bermula dari saat
ia berlaku; ia tidak memulihkan masa lalu.

**Peranti yang gagal tidak pernah dihentikan, walaupun apabila ia kembali.** Satah kawalan
tidak dapat membezakan perakam yang benar-benar mati daripada perakam yang sekadar tidak dapat
dilihatnya. Menghentikan jenis kedua bermakna menghentikan satu-satunya yang sedang merakam,
berdasarkan bukti yang memang tidak lengkap. Jadi paling teruk kedua-dua peranti merakam kamera
yang sama untuk seketika — strim berganda pada kamera dan rakaman berganda — sehingga anda
menyerahkan kamera itu semula. Tiada apa-apa yang merakam ialah satu-satunya keadaan yang tidak
dapat dipulihkan, dan tiada laluan di sini yang boleh menghasilkannya.

Anda diberitahu apabila perakam itu kembali, supaya anda boleh membuat keputusan.

## Membuat pelan {#making}

Pilih perakam yang hendak dilindungi dan peranti simpanan yang melindunginya. Kedua-duanya
mestilah perakam kamera. Satu pelan bagi satu perakam, dan ambil alih tidak berantai: peranti
simpanan tidak boleh pula dilindungi oleh pelan lain, kerana kamera sesebuah tapak yang berakhir
dua peranti jauhnya daripada sesiapa yang mengetahuinya tidak membantu sesiapa.

Satu peranti simpanan boleh melindungi **beberapa** perakam — itulah maksud "+1" dalam N+1. Ia
memerlukan kapasiti untuk menjalankan kesemuanya, yang tidak diukur oleh ujian, dan rangkaian
untuk mencapai kamera mereka, yang memang diukur.

**Tunggu sebelum bertindak** ialah berapa lama perakam mesti hilang hubungan sebelum pelan ini
bertindak. Cukup lama supaya but semula selepas kemas kini tidak mencetuskannya; sekurang-
kurangnya dua minit, kerana perakam tidak diisytiharkan luar talian lebih awal daripada itu,
jadi angka yang lebih pendek ialah janji yang tidak dapat ditunaikan sistem.

**Ambil alih kamera tanpa bertanya** adalah mati melainkan anda menghidupkannya. Jika dibiarkan
mati, pelan memberitahu anda dan menunggu, dan anda menekan satu butang. Jika dihidupkan,
peranti simpanan mula merakam sendiri setelah tempoh menunggu tamat — yang betul apabila perakam
benar-benar mati dan salah apabila anda sekadar tidak dapat melihatnya. Hidupkan selepas ujian
anda sendiri lulus.

## Apabila sebuah perakam berhenti {#takeover}

Anda menerima pemberitahuan, dan kad pelan menyatakan ia bersedia untuk ambil alih. Tekan
**Ambil alih** (atau biarkan pelan yang telah dihidupkan melakukannya) dan peranti simpanan akan
mencipta kamera tersebut lalu mula merakamnya.

Sehingga saat itu tiada apa-apa daripada tapak lain kelihatan pada peranti simpanan: kamera yang
dipentaskan bukan kamera. Ia tidak muncul dalam senarai kamera peranti simpanan, tidak diperiksa
kesihatannya dan tidak dirakam. Peranti simpanan yang melindungi empat perakam tidak memaparkan
kamera empat tapak yang tidak diawasinya.

Ambil alih melaporkan **bagi setiap kamera** apa yang sebenarnya berlaku, dibaca semula daripada
perakam dan bukan diandaikan. Kamera yang sedang merakam menyatakannya. Kamera yang strimnya
tidak dapat dibuka menyatakan hal itu, berserta sebabnya, dan bukan dikira sebagai kejayaan.

## Menyerahkan kamera semula {#failback}

Apabila perakam sihat kembali, tekan **Serah semula**. Peranti simpanan akan berhenti merakam
kamera tersebut.

Ia tidak memadamkannya, dan ia tidak memadamkan rakaman. Segala yang dirakam oleh peranti
simpanan semasa gangguan kekal padanya dan kekal boleh dimainkan — termasuk selepas anda
memadamkan pelan itu sendiri. Rakaman itu ialah satu-satunya rekod bagi tempoh perakam tidak
berfungsi, dan tiada apa-apa di sini yang membuangnya.

## Log masuk kamera {#credentials}

Melindungi sebuah perakam bermakna peranti simpanan perlu log masuk ke kamera perakam itu, jadi
kelayakan tersebut mesti berpindah antara kedua-dua peranti.

Ia bergerak dalam sampul termeterai. Peranti simpanan menghasilkan kunci sekali guna, perakam
memeteraikan senarai kameranya kepada kunci itu dan mengalamatkannya kepada peranti simpanan
tersebut, dan satah kawalan membawanya tanpa mampu membukanya. Sampul yang dipintas dalam
perjalanan tidak boleh dibuka oleh peranti lain, dan tidak boleh diberikan kepada peranti lain.

Mencipta, menguji dan menyerahkan pelan ialah tindakan pentadbir, dan ia direkodkan dalam log
audit — termasuk ambil alih yang berlaku secara automatik, iaitu satu-satunya yang tiada sesiapa
hadir untuknya.
